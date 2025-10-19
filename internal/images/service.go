package images

import (
	"berth-agent/config"
	"berth-agent/internal/docker"
	"berth-agent/internal/stack"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Service struct {
	dockerClient       *docker.Client
	stackService       *stack.Service
	stackLocation      string
	insecureRegistries map[string]bool
}

func NewService(cfg *config.Config, dockerClient *docker.Client, stackService *stack.Service) *Service {
	insecureRegistries := make(map[string]bool)
	ctx := context.Background()
	if info, err := dockerClient.SystemInfo(ctx); err == nil {
		for _, registry := range info.RegistryConfig.InsecureRegistryCIDRs {
			insecureRegistries[registry.String()] = true
			log.Printf("[Image Updates] Detected insecure registry from daemon config: %s", registry.String())
		}
		for registry, indexInfo := range info.RegistryConfig.IndexConfigs {
			if !indexInfo.Secure {
				insecureRegistries[registry] = true
				log.Printf("[Image Updates] Detected insecure registry from daemon config: %s", registry)
			}
		}
	} else {
		log.Printf("[Image Updates] WARNING: Failed to query Docker daemon for insecure registries: %v", err)
	}

	return &Service{
		dockerClient:       dockerClient,
		stackService:       stackService,
		stackLocation:      cfg.StackLocation,
		insecureRegistries: insecureRegistries,
	}
}

func (s *Service) CheckImageUpdates(ctx context.Context, credentials []RegistryCredential, disabledRegistries []string) ([]ContainerImageCheckResult, error) {
	log.Printf("[Image Updates] Starting image update check")
	log.Printf("[Image Updates] Received %d registry credential(s)", len(credentials))
	log.Printf("[Image Updates] Received %d disabled registr(ies)", len(disabledRegistries))

	disabledRegistriesMap := make(map[string]bool)
	for _, registry := range disabledRegistries {
		disabledRegistriesMap[registry] = true
		log.Printf("[Image Updates] Registry '%s' is disabled - will skip checks", registry)
	}

	results := []ContainerImageCheckResult{}

	containers, err := s.getAllRunningContainers(ctx)
	if err != nil {
		log.Printf("[Image Updates] ERROR: Failed to get running containers: %v", err)
		return nil, fmt.Errorf("failed to get running containers: %w", err)
	}

	log.Printf("[Image Updates] Found %d container(s) to check", len(containers))

	containersByStack := make(map[string][]containerInfo)
	for _, container := range containers {
		containersByStack[container.stackName] = append(containersByStack[container.stackName], container)
	}

	log.Printf("[Image Updates] Processing %d stack(s)", len(containersByStack))

	for stackName, stackContainers := range containersByStack {
		log.Printf("[Image Updates] [Stack: %s] Processing %d container(s)", stackName, len(stackContainers))

		neededRegistries := make(map[string]bool)
		for _, container := range stackContainers {
			registry := extractRegistry(container.imageName)
			if !disabledRegistriesMap[registry] {
				neededRegistries[registry] = true
			}
		}

		log.Printf("[Image Updates] [Stack: %s] Containers use %d unique registr(ies)", stackName, len(neededRegistries))
		for registry := range neededRegistries {
			log.Printf("[Image Updates] [Stack: %s] - Registry: %s", stackName, registry)
		}

		stackCredentials := s.filterCredentialsForStackAndRegistries(stackName, neededRegistries, credentials)
		log.Printf("[Image Updates] [Stack: %s] Using %d registry credential(s)", stackName, len(stackCredentials))

		var tempDockerConfig string
		if len(stackCredentials) > 0 {
			tempDockerConfig, err = s.createTempDockerConfig(ctx, stackCredentials)
			if err != nil {
				log.Printf("[Image Updates] [Stack: %s] WARNING: Failed to create temp docker config: %v", stackName, err)
				log.Printf("[Image Updates] [Stack: %s] Continuing without credentials (public registries only)", stackName)
			} else {
				defer os.RemoveAll(tempDockerConfig)
			}
		} else {
			log.Printf("[Image Updates] [Stack: %s] No credentials needed for this stack's registries", stackName)
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

	log.Printf("[Image Updates] Completed image update check: %d success, %d error(s), %d skipped (disabled registries)",
		successCount, errorCount, skippedCount)

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

	log.Printf("[Image Updates] Discovered %d container(s) with compose labels", len(containerList))

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
			log.Printf("[Image Updates] SKIPPED: Container '%s' in stack '%s' - opted out via label (berth.image-update-check.enabled=false)",
				containerName, stackName)
			continue
		}

		currentDigest := ""
		imageName := c.Image
		imageInfo, err := s.dockerClient.ImageInspect(ctx, c.Image)
		if err == nil && len(imageInfo.RepoDigests) > 0 {
			repoDigest := imageInfo.RepoDigests[0]
			log.Printf("[Image Updates] Container '%s' in stack '%s' - current digest from RepoDigests[0]: %s",
				containerName, stackName, repoDigest)

			if idx := strings.Index(repoDigest, "@"); idx > 0 {
				currentDigest = repoDigest[idx+1:]

				if strings.HasPrefix(c.Image, "sha256:") {
					imageName = repoDigest[:idx]
					log.Printf("[Image Updates] Container '%s' in stack '%s' - extracted image name from RepoDigest: '%s'",
						containerName, stackName, imageName)
				}
			} else {
				currentDigest = repoDigest
			}
		} else {
			log.Printf("[Image Updates] Container '%s' in stack '%s' - no RepoDigests found (err: %v, count: %d)",
				containerName, stackName, err, len(imageInfo.RepoDigests))
		}

		containers = append(containers, containerInfo{
			stackName:     stackName,
			containerName: containerName,
			imageName:     imageName,
			currentDigest: currentDigest,
		})
	}

	if skippedCount > 0 {
		log.Printf("[Image Updates] Skipped %d container(s) due to opt-out labels", skippedCount)
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
			log.Printf("[Image Updates] [Stack: %s] Credential for registry '%s' does NOT match stack pattern '%s' - skipping",
				stackName, cred.Registry, cred.StackPattern)
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
			log.Printf("[Image Updates] [Stack: %s] Credential for registry '%s' not needed (stack uses different registries) - skipping",
				stackName, cred.Registry)
			continue
		}

		log.Printf("[Image Updates] [Stack: %s] Using credential for registry '%s' (matches pattern '%s' and is needed)",
			stackName, cred.Registry, cred.StackPattern)
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
		log.Printf("[Image Updates] WARNING: Invalid pattern '%s': %v", pattern, err)
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
		log.Printf("[Image Updates] Authenticating to registry: %s (username: %s)", cred.Registry, cred.Username)

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
			log.Printf("[Image Updates] ERROR: Failed to authenticate to registry %s: %v", cred.Registry, errorMsg)
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("docker login to %s failed: %s", cred.Registry, errorMsg)
		}

		log.Printf("[Image Updates] Successfully authenticated to registry: %s", cred.Registry)
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
		log.Printf("[Image Updates] [Stack: %s] SKIPPED: Container '%s' - image from disabled registry: %s",
			container.stackName, container.containerName, registry)
		result.Error = ""
		return result
	}

	log.Printf("[Image Updates] [Stack: %s] Checking container '%s' - image: %s (registry: %s)",
		container.stackName, container.containerName, container.imageName, registry)

	isInsecure := s.insecureRegistries[registry]
	if isInsecure {
		log.Printf("[Image Updates] [Stack: %s] Registry '%s' is marked as insecure in Docker daemon",
			container.stackName, registry)
	}

	latestDigest, err := s.getLatestImageDigest(ctx, container.imageName, tempDockerConfig, isInsecure)
	if err != nil {
		result.Error = err.Error()
		log.Printf("[Image Updates] [Stack: %s] ERROR checking container '%s': %v",
			container.stackName, container.containerName, err)
		return result
	}

	result.LatestRepoDigest = latestDigest

	if container.currentDigest != "" && latestDigest != "" {
		if container.currentDigest == latestDigest {
			log.Printf("[Image Updates] [Stack: %s] Container '%s' is up to date (digest: %s)",
				container.stackName, container.containerName, truncateDigest(latestDigest))
		} else {
			log.Printf("[Image Updates] [Stack: %s] UPDATE AVAILABLE for container '%s' (current: %s, latest: %s)",
				container.stackName, container.containerName,
				truncateDigest(container.currentDigest), truncateDigest(latestDigest))
		}
	} else {
		log.Printf("[Image Updates] [Stack: %s] Container '%s' check completed (digest information may be incomplete)",
			container.stackName, container.containerName)
	}

	return result
}

func (s *Service) getLatestImageDigest(ctx context.Context, imageName string, tempDockerConfig string, isInsecure bool) (string, error) {
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
				return manifestArray[0].Descriptor.Digest, nil
			}
		}
		return "", fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	if manifestData.Descriptor.Digest != "" {
		return manifestData.Descriptor.Digest, nil
	}

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
