package images

import (
	"berth-agent/config"
	"berth-agent/internal/docker"
	"berth-agent/internal/logging"
	"berth-agent/internal/stack"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

type Service struct {
	dockerClient       *docker.Client
	stackService       *stack.Service
	stackLocation      string
	insecureRegistries map[string]bool
	logger             *logging.Logger
}

func NewService(cfg *config.Config, dockerClient *docker.Client, stackService *stack.Service, logger *logging.Logger) *Service {
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
		stackService:       stackService,
		stackLocation:      cfg.StackLocation,
		insecureRegistries: insecureRegistries,
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

	results := []ContainerImageCheckResult{}

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

	containersByStack := make(map[string][]containerInfo)
	for _, container := range containers {
		containersByStack[container.stackName] = append(containersByStack[container.stackName], container)
	}

	s.logger.Info("Processing stacks",
		zap.Int("stack_count", len(containersByStack)),
	)

	for stackName, stackContainers := range containersByStack {
		s.logger.Info("Processing stack containers",
			zap.String("stack", stackName),
			zap.Int("container_count", len(stackContainers)),
		)

		neededRegistries := make(map[string]bool)
		for _, container := range stackContainers {
			registry := extractRegistry(container.imageName)
			if !disabledRegistriesMap[registry] {
				neededRegistries[registry] = true
			}
		}

		registryList := make([]string, 0, len(neededRegistries))
		for registry := range neededRegistries {
			registryList = append(registryList, registry)
		}
		s.logger.Debug("Stack uses registries",
			zap.String("stack", stackName),
			zap.Int("registry_count", len(neededRegistries)),
			zap.Strings("registries", registryList),
		)

		stackCredentials := s.filterCredentialsForStackAndRegistries(stackName, neededRegistries, credentials)
		s.logger.Debug("Using credentials for stack",
			zap.String("stack", stackName),
			zap.Int("credential_count", len(stackCredentials)),
		)

		var tempDockerConfig string
		if len(stackCredentials) > 0 {
			tempDockerConfig, err = s.createTempDockerConfig(ctx, stackCredentials)
			if err != nil {
				s.logger.Warn("Failed to create temp docker config",
					zap.String("stack", stackName),
					zap.Error(err),
				)
				s.logger.Info("Continuing without credentials (public registries only)",
					zap.String("stack", stackName),
				)
			} else {
				defer os.RemoveAll(tempDockerConfig)
			}
		} else {
			s.logger.Debug("No credentials needed for this stack's registries",
				zap.String("stack", stackName),
			)
		}

		for _, container := range stackContainers {
			result := s.checkContainerImage(ctx, container, tempDockerConfig, disabledRegistriesMap)
			results = append(results, result)
		}
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

type containerInfo struct {
	stackName     string
	containerName string
	imageName     string
	currentDigest string
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

		currentDigest := ""
		imageName := c.Image
		imageInfo, err := s.dockerClient.ImageInspect(ctx, c.Image)
		if err == nil && len(imageInfo.RepoDigests) > 0 {
			repoDigest := imageInfo.RepoDigests[0]
			s.logger.Debug("Found current digest from RepoDigests",
				zap.String("container", containerName),
				zap.String("stack", stackName),
				zap.String("repo_digest", repoDigest),
			)

			if idx := strings.Index(repoDigest, "@"); idx > 0 {
				currentDigest = repoDigest[idx+1:]

				if strings.HasPrefix(c.Image, "sha256:") {
					imageName = repoDigest[:idx]
					s.logger.Debug("Extracted image name from RepoDigest",
						zap.String("container", containerName),
						zap.String("stack", stackName),
						zap.String("image_name", imageName),
					)
				}
			} else {
				currentDigest = repoDigest
			}
		} else {
			s.logger.Debug("No RepoDigests found",
				zap.String("container", containerName),
				zap.String("stack", stackName),
				zap.Error(err),
				zap.Int("digest_count", len(imageInfo.RepoDigests)),
			)
		}

		containers = append(containers, containerInfo{
			stackName:     stackName,
			containerName: containerName,
			imageName:     imageName,
			currentDigest: currentDigest,
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

func (s *Service) filterCredentialsForStackAndRegistries(stackName string, neededRegistries map[string]bool, credentials []RegistryCredential) []RegistryCredential {
	var filtered []RegistryCredential

	for _, cred := range credentials {
		if !matchesPattern(cred.StackPattern, stackName) {
			s.logger.Debug("Credential does not match stack pattern",
				zap.String("stack", stackName),
				zap.String("registry", cred.Registry),
				zap.String("pattern", cred.StackPattern),
			)
			continue
		}

		normalizedCredRegistry := normalizeRegistry(cred.Registry)
		registryNeeded := false
		for neededRegistry := range neededRegistries {
			if normalizeRegistry(neededRegistry) == normalizedCredRegistry {
				registryNeeded = true
				break
			}
		}

		if !registryNeeded {
			s.logger.Debug("Credential not needed for stack registries",
				zap.String("stack", stackName),
				zap.String("registry", cred.Registry),
			)
			continue
		}

		s.logger.Debug("Using credential for registry",
			zap.String("stack", stackName),
			zap.String("registry", cred.Registry),
			zap.String("pattern", cred.StackPattern),
		)
		filtered = append(filtered, cred)
	}

	return filtered
}

func normalizeRegistry(registry string) string {
	normalized := strings.ToLower(strings.TrimSpace(registry))
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

func (s *Service) createTempDockerConfig(ctx context.Context, credentials []RegistryCredential) (string, error) {
	tempDir, err := os.MkdirTemp("", "berth-docker-config-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp docker config directory: %w", err)
	}

	if err := os.Chmod(tempDir, 0700); err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to set permissions on temp docker config: %w", err)
	}

	for _, cred := range credentials {
		s.logger.Debug("Authenticating to registry",
			zap.String("registry", cred.Registry),
			zap.String("username", cred.Username),
		)

		cmd := exec.CommandContext(ctx, "docker", "login", cred.Registry, "-u", cred.Username, "--password-stdin")
		cmd.Env = []string{
			fmt.Sprintf("DOCKER_CONFIG=%s", tempDir),
			"PATH=/usr/local/bin:/usr/bin:/bin",
			"HOME=/tmp",
		}
		cmd.Stdin = strings.NewReader(cred.Password)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			errorMsg := stderr.String()
			if errorMsg == "" {
				errorMsg = err.Error()
			}
			s.logger.Error("Failed to authenticate to registry",
				zap.String("registry", cred.Registry),
				zap.String("error_message", errorMsg),
			)
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("docker login to %s failed: %s", cred.Registry, errorMsg)
		}

		s.logger.Info("Successfully authenticated to registry",
			zap.String("registry", cred.Registry),
		)
	}

	return tempDir, nil
}

func (s *Service) checkContainerImage(ctx context.Context, container containerInfo, tempDockerConfig string, disabledRegistries map[string]bool) ContainerImageCheckResult {
	registry := extractRegistry(container.imageName)

	result := ContainerImageCheckResult{
		StackName:         container.stackName,
		ContainerName:     container.containerName,
		ImageName:         container.imageName,
		CurrentRepoDigest: container.currentDigest,
	}

	if disabledRegistries[registry] {
		s.logger.Info("Container skipped - image from disabled registry",
			zap.String("stack", container.stackName),
			zap.String("container", container.containerName),
			zap.String("registry", registry),
		)
		result.Error = ""
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

	latestDigest, err := s.getLatestImageDigest(ctx, container.imageName, tempDockerConfig, isInsecure)
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

	if container.currentDigest != "" && latestDigest != "" {
		if container.currentDigest == latestDigest {
			s.logger.Debug("Container is up to date",
				zap.String("stack", container.stackName),
				zap.String("container", container.containerName),
				zap.String("digest", truncateDigest(latestDigest)),
			)
		} else {
			s.logger.Warn("Update available for container",
				zap.String("stack", container.stackName),
				zap.String("container", container.containerName),
				zap.String("current_digest", truncateDigest(container.currentDigest)),
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

func (s *Service) getLatestImageDigest(ctx context.Context, imageName string, tempDockerConfig string, isInsecure bool) (string, error) {
	s.logger.Debug("Querying registry for latest image digest",
		zap.String("image", imageName),
		zap.Bool("insecure", isInsecure),
	)

	args := []string{"manifest", "inspect", imageName, "-v"}
	if isInsecure {
		args = append(args, "--insecure")
	}

	cmd := exec.CommandContext(ctx, "docker", args...)

	if tempDockerConfig != "" {
		cmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_CONFIG=%s", tempDockerConfig))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errorMsg := stderr.String()
		if errorMsg == "" {
			errorMsg = err.Error()
		}
		s.logger.Error("Failed to query registry for image manifest",
			zap.String("image", imageName),
			zap.String("error_message", errorMsg),
		)
		return "", fmt.Errorf("failed to inspect image %s: %s", imageName, errorMsg)
	}

	var manifestData struct {
		Descriptor struct {
			Digest string `json:"digest"`
		} `json:"Descriptor"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &manifestData); err != nil {

		var manifestArray []struct {
			Descriptor struct {
				Digest string `json:"digest"`
			} `json:"Descriptor"`
		}
		if err2 := json.Unmarshal(stdout.Bytes(), &manifestArray); err2 == nil && len(manifestArray) > 0 {
			if manifestArray[0].Descriptor.Digest != "" {
				s.logger.Debug("Successfully retrieved image digest from registry",
					zap.String("image", imageName),
					zap.String("digest", truncateDigest(manifestArray[0].Descriptor.Digest)),
				)
				return manifestArray[0].Descriptor.Digest, nil
			}
		}
		s.logger.Error("Failed to parse manifest JSON",
			zap.String("image", imageName),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	if manifestData.Descriptor.Digest != "" {
		s.logger.Debug("Successfully retrieved image digest from registry",
			zap.String("image", imageName),
			zap.String("digest", truncateDigest(manifestData.Descriptor.Digest)),
		)
		return manifestData.Descriptor.Digest, nil
	}

	s.logger.Error("No digest found in manifest",
		zap.String("image", imageName),
	)
	return "", fmt.Errorf("no digest found in manifest for image %s", imageName)
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
