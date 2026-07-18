package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/tech-arch1tect/berth-agent/internal/docker"
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

	lock := s.repoLocks.get(stackName)
	if !lock.TryRLock() {
		return ErrRepositoryBusy
	}
	defer lock.RUnlock()

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

	projectName := s.resolveComposeProjectName(stackName)

	components, err = s.resolveRestoreTargets(ctx, projectName, components)
	if err != nil {
		return err
	}

	orderComponentsForRestore(components)

	volumesToCreate, err := s.validateRestoreTargets(ctx, stackName, stackPath, components)
	if err != nil {
		return err
	}

	if opts.StopMode == "" {
		active, err := s.activeContainerCount(ctx, projectName)
		if err != nil {
			return fmt.Errorf("failed to check for running containers before restore: %w", err)
		}
		if active > 0 {
			return fmt.Errorf("refusing to restore while %d container(s) of stack %s are running, paused or restarting: restoring under a live application corrupts data; stop the stack or request the restore with --stop", active, stackName)
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

	var stopped []string
	if opts.StopMode == "stop" {
		stopped, err = s.stopStackContainers(ctx, projectName, writer)
		if err != nil {
			return fmt.Errorf("failed to stop the stack before restore: %w", err)
		}
	}

	restored, restoreErr := s.runRestore(ctx, image, opts, run, components, volumesToCreate, writer)
	if restoreErr != nil {
		reportRestoreFailure(writer, components, restored)
		return restoreErr
	}

	if len(stopped) > 0 {
		if err := s.startContainers(ctx, stopped, writer); err != nil {
			writer.WriteStderr(fmt.Sprintf("Failed to start the stack after restore: %v", err))
		}
	}

	writer.WriteProgress(fmt.Sprintf("Restore of backup %s completed: %d component(s)", run.ID, len(components)))
	return nil
}

func (s *Service) runRestore(ctx context.Context, image string, opts RestoreOptions, run *Run, components, volumesToCreate []Component, writer ProgressWriter) ([]string, error) {
	for _, component := range volumesToCreate {
		if err := s.createMissingVolume(ctx, component, writer); err != nil {
			return nil, err
		}
	}

	var restored []string
	for _, component := range components {
		if err := s.restoreComponent(ctx, image, opts.Password, run, component, opts.KeepExtraFiles, writer); err != nil {
			return restored, err
		}
		restored = append(restored, component.ID)
	}
	return restored, nil
}

func reportRestoreFailure(writer ProgressWriter, components []Component, restored []string) {
	restoredSet := make(map[string]bool, len(restored))
	for _, id := range restored {
		restoredSet[id] = true
	}
	var notRestored []string
	for _, component := range components {
		if !restoredSet[component.ID] {
			notRestored = append(notRestored, component.ID)
		}
	}

	writer.WriteStderr("Restore failed. The stack has been left stopped because its data is now inconsistent.")
	if len(restored) > 0 {
		writer.WriteStderr("Rolled back to the backup: " + strings.Join(restored, ", "))
	}
	if len(notRestored) > 0 {
		writer.WriteStderr("Left at their previous state: " + strings.Join(notRestored, ", "))
	}
	writer.WriteStderr("Resolve the cause and re-run the restore; start the stack manually only once you have accepted the inconsistency.")
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

func restoringStackDirectory(components []Component) bool {
	for _, component := range components {
		if component.Kind == KindStackDirectory {
			return true
		}
	}
	return false
}

func driftError(components []Component, currentBinds, currentVolumes map[string]bool) error {
	for _, component := range components {
		switch component.Kind {
		case KindBindMount:
			if !currentBinds[component.SourcePath] {
				return fmt.Errorf("cannot restore bind mount %s: its source %q is no longer used by this stack's compose configuration; add it back to the compose file, then retry", component.ID, component.SourcePath)
			}
		case KindVolume:
			if !currentVolumes[component.VolumeName] {
				return fmt.Errorf("cannot restore volume %s: %q is no longer declared in this stack's compose configuration; add it back to the compose file, then retry", component.ID, component.VolumeName)
			}
		}
	}
	return nil
}

func (s *Service) currentComposeTargets(stackName, stackPath string) (binds, volumes map[string]bool, err error) {
	current, _, err := s.composeComponents(stackName, stackPath)
	if err != nil {
		return nil, nil, err
	}
	binds = map[string]bool{}
	volumes = map[string]bool{}
	for _, component := range current {
		switch component.Kind {
		case KindBindMount:
			binds[component.SourcePath] = true
		case KindVolume:
			volumes[component.VolumeName] = true
		}
	}
	return binds, volumes, nil
}

func missingVolumeAction(component Component) (create bool, err error) {
	if component.VolumeDef == nil {
		return false, fmt.Errorf("cannot restore volume %s: it does not exist and this backup was taken before volume definitions were recorded; create the stack's volumes first (docker compose up --no-start), then retry", component.VolumeName)
	}
	if component.VolumeDef.External {
		return false, fmt.Errorf("cannot restore volume %s: external volume %q does not exist and cannot be created automatically; provision it, then retry", component.ID, component.VolumeName)
	}
	return true, nil
}

func (s *Service) planMissingVolumes(ctx context.Context, components []Component) ([]Component, error) {
	var toCreate []Component
	for _, component := range components {
		if component.Kind != KindVolume || component.VolumeName == "" {
			continue
		}
		if _, err := s.dockerClient.InspectVolume(ctx, component.VolumeName); err == nil {
			continue
		}
		create, err := missingVolumeAction(component)
		if err != nil {
			return nil, err
		}
		if create {
			toCreate = append(toCreate, component)
		}
	}
	return toCreate, nil
}

func (s *Service) createMissingVolume(ctx context.Context, component Component, writer ProgressWriter) error {
	def := component.VolumeDef
	writer.WriteProgress("Creating missing volume " + component.VolumeName + "...")
	if _, err := s.dockerClient.CreateVolume(ctx, component.VolumeName, def.Driver, def.DriverOpts, def.Labels); err != nil {
		return fmt.Errorf("failed to create volume %s before restoring into it: %w", component.VolumeName, err)
	}
	writer.WriteStdout("Created volume " + component.VolumeName)
	return nil
}

func (s *Service) validateRestoreTargets(ctx context.Context, stackName, stackPath string, components []Component) ([]Component, error) {
	if !restoringStackDirectory(components) {
		binds, volumes, err := s.currentComposeTargets(stackName, stackPath)
		if err != nil {
			return nil, fmt.Errorf("cannot read the current compose configuration to verify restore targets: %w", err)
		}
		if err := driftError(components, binds, volumes); err != nil {
			return nil, err
		}
	}

	return s.planMissingVolumes(ctx, components)
}

func (s *Service) resolveComposeProjectName(stackName string) string {
	cmd, err := s.commandExec.ExecuteComposeCommand(stackName, "config", "--format", "json")
	if err != nil {
		return stackName
	}
	output, err := cmd.Output()
	if err != nil {
		return stackName
	}
	project, err := parseComposeProject(output)
	if err != nil || project.Name == "" {
		return stackName
	}
	return project.Name
}

func activeContainerStates() []string {
	return []string{"running", "paused", "restarting"}
}

type anonVolumeMatch struct {
	number     string
	volumeName string
}

func resolveAnonVolumeName(component Component, matches []anonVolumeMatch) (string, error) {
	names := map[string]bool{}
	if component.ContainerNumber != "" {
		for _, match := range matches {
			if match.number == component.ContainerNumber {
				names[match.volumeName] = true
			}
		}
		switch len(names) {
		case 0:
			return "", fmt.Errorf("cannot restore %s: no current container of service %q is replica %s, so its anonymous volume no longer exists (recreate the service so the replica is present, then retry)", component.ID, component.Service, component.ContainerNumber)
		case 1:
			return onlyKey(names), nil
		default:
			return "", fmt.Errorf("cannot restore %s: replica %s of service %q maps to more than one anonymous volume; refusing to guess", component.ID, component.ContainerNumber, component.Service)
		}
	}

	for _, match := range matches {
		names[match.volumeName] = true
	}
	switch len(names) {
	case 0:
		return "", fmt.Errorf("cannot restore %s: no container of service %q currently has an anonymous volume at %s (anonymous volumes only exist once their container has been created)", component.ID, component.Service, component.Target)
	case 1:
		return onlyKey(names), nil
	default:
		return "", fmt.Errorf("cannot restore %s: service %q now runs multiple replicas; cannot tell which one this backup's anonymous volume belongs to; scale the service to one replica and retry", component.ID, component.Service)
	}
}

func onlyKey(m map[string]bool) string {
	for key := range m {
		return key
	}
	return ""
}

func (s *Service) resolveAnonymousVolumeName(ctx context.Context, projectName string, component Component) (string, error) {
	containers, err := s.dockerClient.ContainerList(ctx, map[string][]string{
		"label": {
			composeProjectLabel + "=" + projectName,
			composeServiceLabel + "=" + component.Service,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to list containers while resolving the anonymous volume for %s: %w", component.ID, err)
	}

	var matches []anonVolumeMatch
	for _, summary := range containers {
		info, err := s.dockerClient.ContainerInspect(ctx, summary.ID)
		if err != nil {
			return "", fmt.Errorf("failed to inspect container %s while resolving the anonymous volume for %s: %w", summary.ID, component.ID, err)
		}
		for _, containerMount := range info.Mounts {
			if string(containerMount.Type) == "volume" && containerMount.Destination == component.Target && containerMount.Name != "" {
				matches = append(matches, anonVolumeMatch{
					number:     info.Config.Labels[composeContainerNumberLabel],
					volumeName: containerMount.Name,
				})
			}
		}
	}

	return resolveAnonVolumeName(component, matches)
}

func (s *Service) resolveRestoreTargets(ctx context.Context, projectName string, components []Component) ([]Component, error) {
	resolved := make([]Component, 0, len(components))
	for _, component := range components {
		if component.Kind != KindAnonymousVolume {
			resolved = append(resolved, component)
			continue
		}

		name, err := s.resolveAnonymousVolumeName(ctx, projectName, component)
		if err != nil {
			return nil, err
		}
		component.VolumeName = name
		resolved = append(resolved, component)
	}
	return resolved, nil
}

func (s *Service) stopStackContainers(ctx context.Context, projectName string, writer ProgressWriter) ([]string, error) {
	active, err := s.dockerClient.ContainerList(ctx, map[string][]string{
		"label":  {composeProjectLabel + "=" + projectName},
		"status": activeContainerStates(),
	})
	if err != nil {
		return nil, err
	}

	var stopped []string
	for _, summary := range active {
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

func (s *Service) activeContainerCount(ctx context.Context, projectName string) (int, error) {
	containers, err := s.dockerClient.ContainerList(ctx, map[string][]string{
		"label":  {composeProjectLabel + "=" + projectName},
		"status": activeContainerStates(),
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

const helperRestoreParent = "/berth-restore/parent"

func (s *Service) restoreComponent(ctx context.Context, image, password string, run *Run, component Component, keepExtraFiles bool, writer ProgressWriter) error {
	writer.WriteProgress("Restoring " + component.ID + "...")

	if component.Kind == KindBindMount && component.IsFile {
		return s.restoreFileComponent(ctx, image, password, run, component, writer)
	}

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

func (s *Service) restoreFileComponent(ctx context.Context, image, password string, run *Run, component Component, writer ProgressWriter) error {
	parentDir := filepath.Dir(component.SourcePath)
	fileName := filepath.Base(component.SourcePath)
	snapshotFilePath := componentSourceMountPath(component)

	script := `set -e
restic dump "$1" "$2" > "$3"`
	spec := docker.ContainerRunSpec{
		Image:      image,
		Entrypoint: []string{"/bin/sh", "-c", script, "sh", component.SnapshotID, snapshotFilePath, helperRestoreParent + "/" + fileName},
		Env:        resticEnv(password),
		Mounts: []mount.Mount{
			repoMount(s.repoHostPath(run.StackName), false),
			{
				Type:        mount.TypeBind,
				Source:      parentDir,
				Target:      helperRestoreParent,
				BindOptions: &mount.BindOptions{CreateMountpoint: true},
			},
		},
		Labels: s.helperLabels(run.StackName, run.ID),
	}

	var stderr bytes.Buffer
	exitCode, err := s.dockerClient.RunContainer(ctx, spec, io.Discard, &stderr)
	if err != nil {
		return fmt.Errorf("restore of %s failed: %w", component.ID, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("restore of %s failed with exit code %d: %s", component.ID, exitCode, strings.TrimSpace(stderr.String()))
	}

	writer.WriteStdout(fmt.Sprintf("%s: restored file %s", component.ID, fileName))
	return nil
}
