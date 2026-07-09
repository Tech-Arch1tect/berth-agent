package images

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/distribution/reference"
	"github.com/tech-arch1tect/berth-agent/internal/docker"
	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"go.uber.org/zap"
)

type Service struct {
	dockerClient       *docker.Client
	insecureRegistries map[string]bool
	registry           *registryClient
	logger             *logging.Logger
}

func NewService(dockerClient *docker.Client, logger *logging.Logger) *Service {
	insecureRegistries := make(map[string]bool)
	ctx := context.Background()
	if info, err := dockerClient.SystemInfo(ctx); err == nil {
		for _, registry := range info.RegistryConfig.InsecureRegistryCIDRs {
			insecureRegistries[registry.String()] = true
			logger.Info("Detected insecure registry from daemon config",
				zap.String("registry", registry.String()),
			)
		}
		for registry, indexInfo := range info.RegistryConfig.IndexConfigs {
			if !indexInfo.Secure {
				insecureRegistries[registry] = true
				logger.Info("Detected insecure registry from daemon config",
					zap.String("registry", registry),
				)
			}
		}
	} else {
		logger.Warn("Failed to query Docker daemon for insecure registries",
			zap.Error(err),
		)
	}

	return &Service{
		dockerClient:       dockerClient,
		insecureRegistries: insecureRegistries,
		registry:           newRegistryClient(logger),
		logger:             logger,
	}
}

func (s *Service) CheckImageUpdates(ctx context.Context, credentials []RegistryCredential, disabledRegistries []string) ([]ContainerImageCheckResult, error) {
	s.logger.Info("Starting image update check",
		zap.Int("credential_count", len(credentials)),
		zap.Int("disabled_registries_count", len(disabledRegistries)),
	)

	disabledRegistriesMap := make(map[string]bool)
	for _, registry := range disabledRegistries {
		disabledRegistriesMap[registry] = true
		s.logger.Info("Registry disabled - will skip checks",
			zap.String("registry", registry),
		)
	}

	containers, err := s.getAllRunningContainers(ctx)
	if err != nil {
		s.logger.Error("Failed to get running containers",
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to get running containers: %w", err)
	}

	s.logger.Info("Found containers to check",
		zap.Int("container_count", len(containers)),
	)

	results := []ContainerImageCheckResult{}
	for _, container := range containers {
		results = append(results, s.checkContainerImage(ctx, container, credentials, disabledRegistriesMap))
	}

	successCount := 0
	errorCount := 0
	skippedCount := 0
	for _, result := range results {
		if result.Error == "" && result.LatestRepoDigest == "" {
			skippedCount++
		} else if result.Error == "" {
			successCount++
		} else {
			errorCount++
		}
	}

	s.logger.Info("Completed image update check",
		zap.Int("success_count", successCount),
		zap.Int("error_count", errorCount),
		zap.Int("skipped_count", skippedCount),
	)

	return results, nil
}

func (s *Service) RunningContainerImages(ctx context.Context) ([]RunningContainerImage, error) {
	containers, err := s.getAllRunningContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get running containers: %w", err)
	}
	return toRunningContainerImages(containers), nil
}

func toRunningContainerImages(containers []containerInfo) []RunningContainerImage {
	images := make([]RunningContainerImage, 0, len(containers))
	for _, container := range containers {
		images = append(images, RunningContainerImage{
			StackName:     container.stackName,
			ContainerName: container.containerName,
			ImageName:     container.imageName,
			RepoDigests:   container.repoDigests,
		})
	}
	return images
}

type containerInfo struct {
	stackName     string
	containerName string
	imageName     string
	repoDigests   []string
}

func (s *Service) getAllRunningContainers(ctx context.Context) ([]containerInfo, error) {
	filters := map[string][]string{
		"label":  {"com.docker.compose.project"},
		"status": {"running"},
	}

	containerList, err := s.dockerClient.ContainerList(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	s.logger.Debug("Discovered containers with compose labels",
		zap.Int("container_count", len(containerList)),
	)

	var containers []containerInfo
	skippedCount := 0

	for _, c := range containerList {
		stackName := c.Labels["com.docker.compose.project"]
		containerName := c.Labels["com.docker.compose.service"]
		if containerName == "" {
			containerName = c.Names[0]
		}

		if s.isImageUpdateCheckDisabled(c.Labels) {
			skippedCount++
			s.logger.Info("Container opted out of image update checks",
				zap.String("container", containerName),
				zap.String("stack", stackName),
				zap.String("reason", "berth.image-update-check.enabled=false"),
			)
			continue
		}

		imageRef := c.ImageID
		if imageRef == "" {
			imageRef = c.Image
		}
		var repoDigests []string
		if imageInfo, err := s.dockerClient.ImageInspect(ctx, imageRef); err == nil {
			repoDigests = imageInfo.RepoDigests
		} else {
			s.logger.Debug("Failed to inspect container image",
				zap.String("container", containerName),
				zap.String("stack", stackName),
				zap.Error(err),
			)
		}

		imageName := c.Image
		if isImageID(imageName) {
			configImage := ""
			if inspect, err := s.dockerClient.ContainerInspect(ctx, c.ID); err == nil && inspect.Config != nil {
				configImage = inspect.Config.Image
			}
			imageName = resolveImageName(c.Image, configImage, repoDigests)
			if imageName != c.Image {
				s.logger.Debug("Recovered image name for container listed by image ID",
					zap.String("container", containerName),
					zap.String("stack", stackName),
					zap.String("image_name", imageName),
				)
			}
		}

		containers = append(containers, containerInfo{
			stackName:     stackName,
			containerName: containerName,
			imageName:     imageName,
			repoDigests:   repoDigests,
		})
	}

	if skippedCount > 0 {
		s.logger.Info("Skipped containers due to opt-out labels",
			zap.Int("skipped_count", skippedCount),
		)
	}

	return containers, nil
}

func (s *Service) isImageUpdateCheckDisabled(labels map[string]string) bool {
	if val, exists := labels["berth.image-update-check.enabled"]; exists {
		return strings.ToLower(strings.TrimSpace(val)) == "false"
	}
	return false
}

func (s *Service) checkContainerImage(ctx context.Context, container containerInfo, credentials []RegistryCredential, disabledRegistries map[string]bool) ContainerImageCheckResult {
	registry := extractRegistry(container.imageName)

	result := ContainerImageCheckResult{
		StackName:         container.stackName,
		ContainerName:     container.containerName,
		ImageName:         container.imageName,
		CurrentRepoDigest: currentDigestFor(container, ""),
	}

	if disabledRegistries[registry] {
		s.logger.Info("Container skipped - image from disabled registry",
			zap.String("stack", container.stackName),
			zap.String("container", container.containerName),
			zap.String("registry", registry),
		)
		return result
	}

	if strings.Contains(container.imageName, "@") {
		s.logger.Debug("Container skipped - image pinned by digest",
			zap.String("stack", container.stackName),
			zap.String("container", container.containerName),
			zap.String("image", container.imageName),
		)
		return result
	}

	s.logger.Info("Checking container image",
		zap.String("stack", container.stackName),
		zap.String("container", container.containerName),
		zap.String("image", container.imageName),
		zap.String("registry", registry),
	)

	isInsecure := s.insecureRegistries[registry]
	if isInsecure {
		s.logger.Debug("Registry marked as insecure in Docker daemon",
			zap.String("stack", container.stackName),
			zap.String("registry", registry),
		)
	}

	cred := credentialFor(container.stackName, registry, credentials)

	latestDigest, err := s.registry.ResolveTagDigest(ctx, container.imageName, cred, isInsecure)
	if err != nil {
		result.Error = err.Error()
		s.logger.Error("Error checking container image",
			zap.String("stack", container.stackName),
			zap.String("container", container.containerName),
			zap.Error(err),
		)
		return result
	}

	result.LatestRepoDigest = latestDigest
	result.CurrentRepoDigest = currentDigestFor(container, latestDigest)

	if result.CurrentRepoDigest != "" && latestDigest != "" {
		if result.CurrentRepoDigest == latestDigest {
			s.logger.Debug("Container is up to date",
				zap.String("stack", container.stackName),
				zap.String("container", container.containerName),
				zap.String("digest", truncateDigest(latestDigest)),
			)
		} else {
			s.logger.Warn("Update available for container",
				zap.String("stack", container.stackName),
				zap.String("container", container.containerName),
				zap.String("current_digest", truncateDigest(result.CurrentRepoDigest)),
				zap.String("latest_digest", truncateDigest(latestDigest)),
			)
		}
	} else {
		s.logger.Debug("Container check completed with incomplete digest information",
			zap.String("stack", container.stackName),
			zap.String("container", container.containerName),
		)
	}

	return result
}

func resolveImageName(listImage, configImage string, repoDigests []string) string {
	if !isImageID(listImage) {
		return listImage
	}
	if configImage != "" && !isImageID(configImage) {
		return configImage
	}
	for _, entry := range repoDigests {
		if name, _, ok := strings.Cut(entry, "@"); ok && name != "" {
			return name
		}
	}
	return listImage
}

func currentDigestFor(container containerInfo, latestDigest string) string {
	repo := ""
	if named, err := reference.ParseNormalizedNamed(container.imageName); err == nil {
		repo = reference.FamiliarName(named)
	}

	first := ""
	fromMatchingRepo := ""
	for _, entry := range container.repoDigests {
		name, digest, ok := strings.Cut(entry, "@")
		if !ok || digest == "" {
			continue
		}
		if first == "" {
			first = digest
		}
		if latestDigest != "" && digest == latestDigest {
			return digest
		}
		if repo != "" && fromMatchingRepo == "" {
			if entryNamed, err := reference.ParseNormalizedNamed(name); err == nil && reference.FamiliarName(entryNamed) == repo {
				fromMatchingRepo = digest
			}
		}
	}

	if fromMatchingRepo != "" {
		return fromMatchingRepo
	}
	return first
}

func credentialFor(stackName, registry string, credentials []RegistryCredential) *RegistryCredential {
	normalizedRegistry := normalizeRegistry(registry)
	for i := range credentials {
		cred := &credentials[i]
		if normalizeRegistry(cred.Registry) != normalizedRegistry {
			continue
		}
		if !matchesPattern(cred.StackPattern, stackName) {
			continue
		}
		return cred
	}
	return nil
}

func normalizeRegistry(registry string) string {
	normalized := strings.ToLower(strings.TrimSpace(registry))
	normalized = strings.TrimPrefix(normalized, "https://")
	normalized = strings.TrimPrefix(normalized, "http://")
	normalized = strings.TrimSuffix(normalized, "/")
	if normalized == "index.docker.io" || normalized == "registry-1.docker.io" {
		return "docker.io"
	}
	return normalized
}

func matchesPattern(pattern, name string) bool {
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}

	matched, err := filepath.Match(pattern, name)
	if err != nil {
		return false
	}
	return matched
}

func isImageID(s string) bool {
	s = strings.TrimPrefix(s, "sha256:")
	if len(s) < 12 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func extractRegistry(imageName string) string {
	parts := strings.SplitN(imageName, "/", 2)

	if len(parts) == 1 {
		return "docker.io"
	}

	if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
		return parts[0]
	}

	return "docker.io"
}

func truncateDigest(digest string) string {
	if digest == "" {
		return "<none>"
	}

	if strings.HasPrefix(digest, "sha256:") && len(digest) > 19 {
		return digest[:19] + "..."
	}

	if len(digest) > 16 {
		return digest[:16] + "..."
	}

	return digest
}
