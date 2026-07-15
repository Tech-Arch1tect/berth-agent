package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type composeVolumeEntry struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type composeServiceConfig struct {
	Volumes []composeVolumeEntry `json:"volumes"`
}

type composeVolumeConfig struct {
	Name       string            `json:"name"`
	External   bool              `json:"external"`
	Driver     string            `json:"driver"`
	DriverOpts map[string]string `json:"driver_opts"`
	Labels     map[string]string `json:"labels"`
}

type composeProject struct {
	Name     string                          `json:"name"`
	Services map[string]composeServiceConfig `json:"services"`
	Volumes  map[string]composeVolumeConfig  `json:"volumes"`
}

func parseComposeProject(data []byte) (*composeProject, error) {
	var project composeProject
	if err := json.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("failed to parse compose config output: %w", err)
	}
	return &project, nil
}

func BuildComponents(project *composeProject, stackPath, backupLocation string) ([]Component, []SkippedMount, error) {
	stackPath = filepath.Clean(stackPath)
	backupLocation = filepath.Clean(backupLocation)

	if err := checkNoOverlap(backupLocation, stackPath, "stack directory"); err != nil {
		return nil, nil, err
	}

	var skipped []SkippedMount
	bindSources := map[string]bool{}
	volumeDefs := map[string]composeVolumeConfig{}
	anonymousKeys := map[string]Component{}

	serviceNames := make([]string, 0, len(project.Services))
	for name := range project.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	for _, serviceName := range serviceNames {
		for _, entry := range project.Services[serviceName].Volumes {
			switch entry.Type {
			case "bind":
				source := filepath.Clean(entry.Source)
				if !filepath.IsAbs(source) {
					return nil, nil, fmt.Errorf("bind mount source %q for service %q is not an absolute path; refusing to back up an unresolvable path", entry.Source, serviceName)
				}
				if source == stackPath {
					skipped = append(skipped, SkippedMount{
						Kind:    "bind",
						Service: serviceName,
						Target:  entry.Target,
						Reason:  "bind mount of the stack directory itself; its data is captured by the stack-directory component",
					})
					continue
				}
				if err := checkNoOverlap(backupLocation, source, "bind mount source"); err != nil {
					return nil, nil, err
				}
				bindSources[source] = true
			case "volume":
				if entry.Source == "" {
					component := Component{
						ID:      string(KindAnonymousVolume) + ":" + serviceName + ":" + entry.Target,
						Kind:    KindAnonymousVolume,
						Service: serviceName,
						Target:  entry.Target,
					}
					anonymousKeys[component.ID] = component
					continue
				}
				volumeConfig, declared := project.Volumes[entry.Source]
				if !declared || volumeConfig.Name == "" {
					return nil, nil, fmt.Errorf("volume %q used by service %q has no resolved name in the compose configuration; refusing to guess which volume to back up", entry.Source, serviceName)
				}
				volumeDefs[volumeConfig.Name] = volumeConfig
			case "tmpfs":
				skipped = append(skipped, SkippedMount{
					Kind:    "tmpfs",
					Service: serviceName,
					Target:  entry.Target,
					Reason:  "tmpfs mounts hold no durable data",
				})
			default:
				return nil, nil, fmt.Errorf("service %q declares a mount of unsupported type %q at %q; refusing to back up a stack with mounts the backup cannot represent", serviceName, entry.Type, entry.Target)
			}
		}
	}

	sortedBinds := make([]string, 0, len(bindSources))
	for source := range bindSources {
		sortedBinds = append(sortedBinds, source)
	}
	sort.Strings(sortedBinds)

	stackDir := Component{
		ID:         string(KindStackDirectory),
		Kind:       KindStackDirectory,
		SourcePath: stackPath,
	}
	for _, source := range sortedBinds {
		if within, rel := pathWithin(stackPath, source); within {
			stackDir.Excludes = append(stackDir.Excludes, rel)
		}
	}

	components := []Component{stackDir}

	for _, source := range sortedBinds {
		component := Component{
			ID:         string(KindBindMount) + ":" + source,
			Kind:       KindBindMount,
			SourcePath: source,
		}
		for _, other := range sortedBinds {
			if other == source {
				continue
			}
			if within, rel := pathWithin(source, other); within {
				component.Excludes = append(component.Excludes, rel)
			}
		}
		components = append(components, component)
	}

	sortedVolumes := make([]string, 0, len(volumeDefs))
	for name := range volumeDefs {
		sortedVolumes = append(sortedVolumes, name)
	}
	sort.Strings(sortedVolumes)
	for _, name := range sortedVolumes {
		config := volumeDefs[name]
		components = append(components, Component{
			ID:         string(KindVolume) + ":" + name,
			Kind:       KindVolume,
			VolumeName: name,
			VolumeDef: &VolumeDefinition{
				External:   config.External,
				Driver:     config.Driver,
				DriverOpts: config.DriverOpts,
				Labels:     config.Labels,
			},
		})
	}

	sortedAnonymous := make([]string, 0, len(anonymousKeys))
	for key := range anonymousKeys {
		sortedAnonymous = append(sortedAnonymous, key)
	}
	sort.Strings(sortedAnonymous)
	for _, key := range sortedAnonymous {
		components = append(components, anonymousKeys[key])
	}

	return components, skipped, nil
}

func checkNoOverlap(backupLocation, sourcePath, description string) error {
	if backupLocation == sourcePath {
		return fmt.Errorf("backup location %q is the same path as the %s; refusing to back up the backup repository into itself", backupLocation, description)
	}
	if within, _ := pathWithin(sourcePath, backupLocation); within {
		return fmt.Errorf("backup location %q is inside the %s %q; refusing to back up the backup repository into itself", backupLocation, description, sourcePath)
	}
	if within, _ := pathWithin(backupLocation, sourcePath); within {
		return fmt.Errorf("%s %q is inside the backup location %q; refusing a backup that would read its own repository", description, sourcePath, backupLocation)
	}
	return nil
}

func pathWithin(parent, child string) (bool, string) {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false, ""
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, ""
	}
	return true, rel
}

func componentMountName(c Component) string {
	identity := c.ID
	sum := sha256.Sum256([]byte(identity))
	return string(c.Kind) + "-" + hex.EncodeToString(sum[:])[:12]
}
