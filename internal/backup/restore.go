package backup

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/mount"
)

type RestoreOptions struct {
	BackupID       string
	ComponentIDs   []string
	StopMode       string
	KeepExtraFiles bool
	Password       string
}

func (s *Service) RestoreBackup(ctx context.Context, stackName, stackPath string, opts RestoreOptions, writer ProgressWriter) error {
	ctx = context.WithoutCancel(ctx)

	if err := s.validateConfiguration(); err != nil {
		return err
	}

	run, err := s.GetRun(stackName, opts.BackupID)
	if err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("backup %s does not exist for stack %s", opts.BackupID, stackName)
	}

	components, notRestorable, err := selectRestoreComponents(run, opts.ComponentIDs)
	if err != nil {
		return err
	}
	for _, note := range notRestorable {
		writer.WriteStdout("Not restorable: " + note)
	}

	image, err := s.helperImage(ctx)
	if err != nil {
		return err
	}

	components, err = s.resolveRestoreTargets(ctx, stackName, components)
	if err != nil {
		return err
	}

	orderComponentsForRestore(components)

	if opts.StopMode == "" {
		running, err := s.runningContainerCount(ctx, stackName)
		if err != nil {
			return fmt.Errorf("failed to check for running containers before restore: %w", err)
		}
		if running > 0 {
			return fmt.Errorf("refusing to restore while %d container(s) of stack %s are running: restoring under a live application corrupts data; stop the stack or request the restore with --stop", running, stackName)
		}
	}

	for _, component := range components {
		writer.WriteStdout("Will restore: " + component.ID)
	}
	if opts.KeepExtraFiles {
		writer.WriteStdout("Files created after the backup will be kept")
	} else {
		writer.WriteStdout("Files created after the backup will be removed (exact snapshot state)")
	}

	if err := s.openRepositoryForRestore(ctx, image, opts.Password, run); err != nil {
		return err
	}

	if opts.StopMode == "stop" {
		stopped, err := s.stopStackContainers(ctx, stackName, writer)
		if err != nil {
			return fmt.Errorf("failed to stop the stack before restore: %w", err)
		}
		defer func() {
			if err := s.startContainers(ctx, stopped, writer); err != nil {
				writer.WriteStderr(fmt.Sprintf("Failed to start the stack after restore: %v", err))
			}
		}()
	}

	for _, component := range components {
		if err := s.restoreComponent(ctx, image, opts.Password, run, component, opts.KeepExtraFiles, writer); err != nil {
			return err
		}
	}

	writer.WriteProgress(fmt.Sprintf("Restore of backup %s completed: %d component(s)", run.ID, len(components)))
	return nil
}

func selectRestoreComponents(run *Run, requested []string) ([]Component, []string, error) {
	byID := make(map[string]Component, len(run.Components))
	for _, component := range run.Components {
		byID[component.ID] = component
	}

	if len(requested) > 0 {
		selected := make([]Component, 0, len(requested))
		seen := map[string]bool{}
		for _, id := range requested {
			if seen[id] {
				continue
			}
			seen[id] = true
			component, exists := byID[id]
			if !exists {
				return nil, nil, fmt.Errorf("backup %s has no component %q", run.ID, id)
			}
			if component.SnapshotID == "" {
				return nil, nil, fmt.Errorf("component %q has no snapshot in backup %s (it failed during the backup) and cannot be restored", id, run.ID)
			}
			selected = append(selected, component)
		}
		return selected, nil, nil
	}

	var selected []Component
	var notRestorable []string
	for _, component := range run.Components {
		if component.SnapshotID == "" {
			notRestorable = append(notRestorable, fmt.Sprintf("%s (no snapshot was taken: %s)", component.ID, component.Error))
			continue
		}
		selected = append(selected, component)
	}
	if len(selected) == 0 {
		return nil, nil, fmt.Errorf("backup %s contains no restorable components", run.ID)
	}
	return selected, notRestorable, nil
}

func (s *Service) resolveRestoreTargets(ctx context.Context, stackName string, components []Component) ([]Component, error) {
	resolved := make([]Component, 0, len(components))
	for _, component := range components {
		if component.Kind != KindAnonymousVolume {
			resolved = append(resolved, component)
			continue
		}

		containers, err := s.dockerClient.ContainerList(ctx, map[string][]string{
			"label": {
				composeProjectLabel + "=" + stackName,
				composeServiceLabel + "=" + component.Service,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list containers while resolving the anonymous volume for %s: %w", component.ID, err)
		}

		currentName := ""
		for _, summary := range containers {
			info, err := s.dockerClient.ContainerInspect(ctx, summary.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to inspect container %s while resolving the anonymous volume for %s: %w", summary.ID, component.ID, err)
			}
			for _, containerMount := range info.Mounts {
				if string(containerMount.Type) == "volume" && containerMount.Destination == component.Target && containerMount.Name != "" {
					currentName = containerMount.Name
				}
			}
		}
		if currentName == "" {
			return nil, fmt.Errorf("cannot restore %s: no container of service %q currently has an anonymous volume at %s (anonymous volumes only exist once their container has been created)", component.ID, component.Service, component.Target)
		}
		component.VolumeName = currentName
		resolved = append(resolved, component)
	}
	return resolved, nil
}

func (s *Service) stopStackContainers(ctx context.Context, stackName string, writer ProgressWriter) ([]string, error) {
	running, err := s.dockerClient.ContainerList(ctx, map[string][]string{
		"label":  {composeProjectLabel + "=" + stackName},
		"status": {"running"},
	})
	if err != nil {
		return nil, err
	}

	var stopped []string
	for _, summary := range running {
		name := containerDisplayName(summary.Names, summary.ID)
		writer.WriteProgress("Stopping container " + name + "...")
		if err := s.dockerClient.ContainerStop(ctx, summary.ID); err != nil {
			return stopped, err
		}
		writer.WriteStdout("Stopped " + name)
		stopped = append(stopped, summary.ID)
	}
	if len(stopped) == 0 {
		writer.WriteStdout("No running containers to stop")
	}
	return stopped, nil
}

func (s *Service) startContainers(ctx context.Context, containerIDs []string, writer ProgressWriter) error {
	if len(containerIDs) == 0 {
		return nil
	}
	writer.WriteProgress("Starting the stack's containers again...")
	for i := len(containerIDs) - 1; i >= 0; i-- {
		if err := s.dockerClient.ContainerStart(ctx, containerIDs[i]); err != nil {
			return err
		}
	}
	writer.WriteStdout(fmt.Sprintf("Started %d container(s)", len(containerIDs)))
	return nil
}

func containerDisplayName(names []string, id string) string {
	if len(names) > 0 {
		return strings.TrimPrefix(names[0], "/")
	}
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}

func (s *Service) runningContainerCount(ctx context.Context, stackName string) (int, error) {
	containers, err := s.dockerClient.ContainerList(ctx, map[string][]string{
		"label":  {composeProjectLabel + "=" + stackName},
		"status": {"running"},
	})
	if err != nil {
		return 0, err
	}
	return len(containers), nil
}

func (s *Service) openRepositoryForRestore(ctx context.Context, image, password string, run *Run) error {
	repoPath := s.repoHostPath(run.StackName)

	probe, err := s.runResticBuffered(ctx, image, run.StackName, run.ID, password, []string{"cat", "config"}, []mount.Mount{repoMount(repoPath, false)})
	if err != nil {
		return fmt.Errorf("failed to probe the backup repository: %w", err)
	}
	switch probe.exitCode {
	case 0:
	case resticExitRepoDoesNotExist:
		return fmt.Errorf("no backup repository exists for stack %q; nothing can be restored", run.StackName)
	case resticExitWrongPassword:
		return fmt.Errorf("the backup password configured for this server in berth does not open the repository for stack %q", run.StackName)
	default:
		return fmt.Errorf("backup repository probe failed with exit code %d: %s", probe.exitCode, probe.output)
	}

	unlock, err := s.runResticBuffered(ctx, image, run.StackName, run.ID, password, []string{"unlock"}, []mount.Mount{repoMount(repoPath, false)})
	if err != nil {
		return fmt.Errorf("failed to clear stale repository locks: %w", err)
	}
	if unlock.exitCode != 0 {
		return fmt.Errorf("clearing stale repository locks failed with exit code %d: %s", unlock.exitCode, unlock.output)
	}
	return nil
}

func restoreArgs(component Component, keepExtraFiles bool) []string {
	sourcePath := componentSourceMountPath(component)
	args := []string{
		"restore",
		component.SnapshotID + ":" + sourcePath,
		"--target", sourcePath,
		"--json",
	}
	if !keepExtraFiles {
		args = append(args, "--delete")
		for _, exclude := range component.Excludes {
			args = append(args, "--exclude", "/"+exclude)
		}
	}
	return args
}

func restoreKindRank(kind ComponentKind) int {
	switch kind {
	case KindStackDirectory:
		return 0
	case KindBindMount:
		return 1
	case KindVolume:
		return 2
	case KindAnonymousVolume:
		return 3
	default:
		return 4
	}
}

func orderComponentsForRestore(components []Component) {
	sort.SliceStable(components, func(i, j int) bool {
		return restoreKindRank(components[i].Kind) < restoreKindRank(components[j].Kind)
	})
}

func restoreTargetMount(component Component) (mount.Mount, error) {
	target := componentSourceMountPath(component)
	switch component.Kind {
	case KindStackDirectory, KindBindMount:
		return mount.Mount{
			Type:   mount.TypeBind,
			Source: component.SourcePath,
			Target: target,
			BindOptions: &mount.BindOptions{
				CreateMountpoint: true,
			},
		}, nil
	case KindVolume, KindAnonymousVolume:
		if component.VolumeName == "" {
			return mount.Mount{}, fmt.Errorf("component %s has no resolved volume name to restore into", component.ID)
		}
		return mount.Mount{Type: mount.TypeVolume, Source: component.VolumeName, Target: target}, nil
	default:
		return mount.Mount{}, fmt.Errorf("component %s has unsupported kind %s", component.ID, component.Kind)
	}
}

func (s *Service) restoreComponent(ctx context.Context, image, password string, run *Run, component Component, keepExtraFiles bool, writer ProgressWriter) error {
	writer.WriteProgress("Restoring " + component.ID + "...")

	targetMount, err := restoreTargetMount(component)
	if err != nil {
		return err
	}
	mounts := []mount.Mount{repoMount(s.repoHostPath(run.StackName), false), targetMount}

	parser := newResticOutputParser(component.ID, writer)
	exitCode, err := s.runResticStreaming(ctx, image, run.StackName, run.ID, password, restoreArgs(component, keepExtraFiles), mounts,
		parser.handleLine,
		writer.WriteStderr,
	)
	if err != nil {
		return fmt.Errorf("restore of %s failed: %w", component.ID, err)
	}
	if exitCode != 0 {
		message := fmt.Sprintf("restore of %s failed with restic exit code %d", component.ID, exitCode)
		if len(parser.errors) > 0 {
			message += ": " + strings.Join(parser.errors, "; ")
		}
		return fmt.Errorf("%s", message)
	}

	writer.WriteStdout(fmt.Sprintf("%s: restored snapshot %s", component.ID, component.SnapshotID[:8]))
	return nil
}
