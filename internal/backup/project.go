package backup

import (
	"context"
	"fmt"
	"path/filepath"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/tech-arch1tect/berth-agent/internal/docker"
)

type stackIdentity struct {
	ProjectName string
	StackPath   string
	Identified  bool
}

func (s *Service) composeProjectName(stackName string) (string, bool) {
	cmd, err := s.commandExec.ExecuteComposeCommand(stackName, "config", "--format", "json")
	if err != nil {
		return "", false
	}
	output, err := cmd.Output()
	if err != nil {
		return "", false
	}
	project, err := parseComposeProject(output)
	if err != nil || project.Name == "" {
		return "", false
	}
	return project.Name, true
}

func (s *Service) resolveStackIdentity(ctx context.Context, stackName, stackPath string) (stackIdentity, error) {
	identity := stackIdentity{StackPath: filepath.Clean(stackPath)}

	if name, ok := s.composeProjectName(stackName); ok {
		identity.ProjectName = name
		identity.Identified = true
		return identity, nil
	}

	labelled, err := s.dockerClient.ContainerList(ctx, map[string][]string{
		"label": {docker.LabelComposeWorkingDir + "=" + identity.StackPath},
	})
	if err != nil {
		return identity, fmt.Errorf("failed to look up the containers of stack %q: %w", stackName, err)
	}
	if len(labelled) > 0 {
		identity.ProjectName = labelled[0].Labels[docker.LabelComposeProject]
		identity.Identified = true
	}
	return identity, nil
}

func (s *Service) listStackContainers(ctx context.Context, identity stackIdentity) ([]dockercontainer.Summary, error) {
	byID := map[string]dockercontainer.Summary{}

	queries := [][]string{{docker.LabelComposeWorkingDir + "=" + identity.StackPath}}
	if identity.ProjectName != "" {
		queries = append(queries, []string{docker.LabelComposeProject + "=" + identity.ProjectName})
	}

	for _, labels := range queries {
		found, err := s.dockerClient.ContainerList(ctx, map[string][]string{"label": labels})
		if err != nil {
			return nil, fmt.Errorf("failed to list the containers of stack %q: %w", identity.ProjectName, err)
		}
		for _, summary := range found {
			byID[summary.ID] = summary
		}
	}

	containers := make([]dockercontainer.Summary, 0, len(byID))
	for _, summary := range byID {
		containers = append(containers, summary)
	}
	return containers, nil
}

func containersForService(containers []dockercontainer.Summary, service string) []dockercontainer.Summary {
	matched := make([]dockercontainer.Summary, 0, len(containers))
	for _, summary := range containers {
		if summary.Labels[docker.LabelComposeService] == service {
			matched = append(matched, summary)
		}
	}
	return matched
}

func containerIsActive(state string) bool {
	switch state {
	case "running", "paused", "restarting":
		return true
	}
	return false
}

func activeContainers(containers []dockercontainer.Summary) []dockercontainer.Summary {
	active := make([]dockercontainer.Summary, 0, len(containers))
	for _, summary := range containers {
		if containerIsActive(string(summary.State)) {
			active = append(active, summary)
		}
	}
	return active
}

func anyAnonymousVolumes(components []Component) bool {
	for _, component := range components {
		if component.Kind == KindAnonymousVolume {
			return true
		}
	}
	return false
}
