package stacks

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

func ScanStacks(basePath string) ([]Stack, error) {
	var stacks []Stack
	ctx := context.Background()

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("Failed to create Docker client: %v", err)
	}
	defer func() {
		if dockerClient != nil {
			dockerClient.Close()
		}
	}()

	dockerNetworks, _ := getDockerNetworks(ctx, dockerClient)

	err = filepath.WalkDir(basePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if !d.IsDir() {
			return nil
		}

		composeFiles := []string{
			"docker-compose.yml",
			"docker-compose.yaml",
			"compose.yml",
			"compose.yaml",
		}

		for _, composeFile := range composeFiles {
			composePath := filepath.Join(path, composeFile)
			if _, err := os.Stat(composePath); err == nil {
				stackName := filepath.Base(path)
				if stackName == filepath.Base(basePath) {
					stackName = "root"
				}

				stack := Stack{
					Name: stackName,
					Path: path,
				}

				options, err := cli.NewProjectOptions(
					[]string{composePath},
					cli.WithOsEnv,
					cli.WithDotEnv,
					cli.WithName(stackName),
				)
				if err == nil {
					project, err := options.LoadProject(ctx)
					if err == nil {
						stack.Services = project.Services
						if dockerClient != nil && dockerNetworks != nil {
							stack.Networks = enhanceNetworksWithRuntimeInfo(dockerNetworks, project.Networks, stackName)
						} else {
							stack.Networks = make(map[string]EnhancedNetworkConfig)
							for name, config := range project.Networks {
								stack.Networks[name] = EnhancedNetworkConfig{
									ComposeConfig: config,
								}
							}
						}
						stack.Volumes = project.Volumes
						stack.ParsedSuccessfully = true
					} else {
						stack.ParsedSuccessfully = false
					}
				} else {
					stack.ParsedSuccessfully = false
					log.Printf("Error parsing stack %s: %v", stackName, err)
				}

				stacks = append(stacks, stack)
				break
			}
		}

		return nil
	})

	return stacks, err
}

func getDockerNetworks(ctx context.Context, dockerClient *client.Client) ([]dockertypes.NetworkResource, error) {
	var networks []dockertypes.NetworkResource
	if dockerClient == nil {
		return networks, nil
	}

	networks, err := dockerClient.NetworkList(ctx, dockertypes.NetworkListOptions{})
	if err != nil {
		log.Printf("Failed to list Docker networks: %v", err)
	}
	return networks, nil
}

func enhanceNetworksWithRuntimeInfo(dockerNetworks []dockertypes.NetworkResource, composeNetworks map[string]types.NetworkConfig, stackName string) map[string]EnhancedNetworkConfig {
	enhanced := make(map[string]EnhancedNetworkConfig)

	for name, config := range composeNetworks {
		enhanced[name] = EnhancedNetworkConfig{
			ComposeConfig: config,
		}
	}

	if dockerNetworks == nil {
		return enhanced
	}

	for _, dockerNet := range dockerNetworks {
		var matchedComposeName string

		if _, exists := enhanced[dockerNet.Name]; exists {
			matchedComposeName = dockerNet.Name
		} else {
			prefix := stackName + "_"
			if strings.HasPrefix(dockerNet.Name, prefix) {
				composeName := strings.TrimPrefix(dockerNet.Name, prefix)
				if _, exists := enhanced[composeName]; exists {
					matchedComposeName = composeName
				}
			}

			if dockerNet.Name == stackName+"_default" {
				if _, exists := enhanced["default"]; !exists {
					enhanced["default"] = EnhancedNetworkConfig{
						ComposeConfig: types.NetworkConfig{
							Name: "default",
						},
					}
				}
				matchedComposeName = "default"
			}
		}

		if matchedComposeName != "" {
			if existing, exists := enhanced[matchedComposeName]; exists {
				existing.RuntimeInfo = &RuntimeNetworkInfo{
					Name:     dockerNet.Name,
					ID:       dockerNet.ID,
					Driver:   dockerNet.Driver,
					Scope:    dockerNet.Scope,
					Internal: dockerNet.Internal,
					IPAM:     dockerNet.IPAM,
					Options:  dockerNet.Options,
					Labels:   dockerNet.Labels,
					Created:  dockerNet.Created.String(),
				}
				enhanced[matchedComposeName] = existing
			}
		}
	}

	return enhanced
}
