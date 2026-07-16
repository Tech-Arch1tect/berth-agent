package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/google/uuid"
	"github.com/tech-arch1tect/berth-agent/config"
	"github.com/tech-arch1tect/berth-agent/internal/docker"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"go.uber.org/zap"
)

const (
	composeProjectLabel         = "com.docker.compose.project"
	composeServiceLabel         = "com.docker.compose.service"
	composeContainerNumberLabel = "com.docker.compose.container-number"
)

type Service struct {
	cfg          *config.Config
	logger       *logging.Logger
	dockerClient *docker.Client
	commandExec  *docker.CommandExecutor
	persistence  *RunPersistence
}

func NewService(cfg *config.Config, logger *logging.Logger, dockerClient *docker.Client, commandExec *docker.CommandExecutor) (*Service, error) {
	persistence, err := NewRunPersistence(cfg.BackupPersistenceDir, logger)
	if err != nil {
		return nil, err
	}
	return &Service{
		cfg:          cfg,
		logger:       logger,
		dockerClient: dockerClient,
		commandExec:  commandExec,
		persistence:  persistence,
	}, nil
}

func (s *Service) Configured() bool {
	return s.cfg.BackupLocation != ""
}

func (s *Service) validateConfiguration() error {
	if s.cfg.BackupLocation == "" {
		return fmt.Errorf("backups are not configured on this agent: set BACKUP_LOCATION in the agent environment")
	}
	if !filepath.IsAbs(s.cfg.BackupLocation) {
		return fmt.Errorf("BACKUP_LOCATION %q must be an absolute host path", s.cfg.BackupLocation)
	}
	if err := checkNoOverlap(filepath.Clean(s.cfg.BackupLocation), filepath.Clean(s.cfg.StackLocation), "stack location"); err != nil {
		return err
	}
	return nil
}

func (s *Service) CreateBackup(ctx context.Context, stackName, stackPath string, opts CreateOptions, writer ProgressWriter) error {
	ctx = context.WithoutCancel(ctx)

	if err := s.validateConfiguration(); err != nil {
		return err
	}
	if opts.StopMode != "" && opts.StopMode != "stop" && opts.StopMode != "pause" {
		return fmt.Errorf("unsupported stop mode %q", opts.StopMode)
	}

	image, err := s.helperImage(ctx)
	if err != nil {
		return err
	}

	writer.WriteProgress("Enumerating backup components from the compose configuration...")
	components, skipped, err := s.enumerateComponents(ctx, stackName, stackPath)
	if err != nil {
		return err
	}

	run := &Run{
		ID:         uuid.New().String(),
		StackName:  stackName,
		StartedAt:  time.Now(),
		Status:     StatusRunning,
		StopMode:   opts.StopMode,
		Components: components,
		Skipped:    skipped,
	}

	if err := s.precheckSources(ctx, image, run); err != nil {
		return err
	}

	for _, component := range run.Components {
		writer.WriteStdout("Will back up: " + component.ID)
	}
	for _, skip := range run.Skipped {
		writer.WriteStdout(fmt.Sprintf("Skipping %s mount at %s (%s): %s", skip.Kind, skip.Target, skip.Service, skip.Reason))
	}

	if err := s.persistence.PersistRun(run); err != nil {
		return err
	}

	runErr := s.executeRun(ctx, image, stackPath, opts.Password, run, writer)
	if runErr == nil {
		runErr = s.verifyRepository(ctx, image, opts.Password, run, writer)
	}

	now := time.Now()
	run.FinishedAt = &now
	if runErr != nil {
		run.Status = StatusFailed
		run.Error = runErr.Error()
	} else {
		run.Status = StatusCompleted
	}
	if err := s.persistence.PersistRun(run); err != nil {
		s.logger.Error("failed to persist final backup run metadata",
			zap.String("run_id", run.ID),
			zap.String("stack_name", run.StackName),
			zap.Error(err),
		)
		if runErr == nil {
			runErr = err
		}
	}

	if runErr == nil {
		writer.WriteProgress(fmt.Sprintf("Backup %s completed: %d components", run.ID, len(run.Components)))
	}
	return runErr
}

func (s *Service) executeRun(ctx context.Context, image, stackPath, password string, run *Run, writer ProgressWriter) error {
	if run.StopMode != "" {
		stopCommand, startCommand := "stop", "start"
		if run.StopMode == "pause" {
			stopCommand, startCommand = "pause", "unpause"
		}
		writer.WriteProgress(fmt.Sprintf("Running docker compose %s before backup...", stopCommand))
		if err := s.runComposeLifecycle(ctx, stackPath, stopCommand, writer); err != nil {
			return fmt.Errorf("failed to %s the stack before backup: %w", stopCommand, err)
		}
		defer func() {
			writer.WriteProgress(fmt.Sprintf("Running docker compose %s after backup...", startCommand))
			if err := s.runComposeLifecycle(ctx, stackPath, startCommand, writer); err != nil {
				writer.WriteStderr(fmt.Sprintf("Failed to %s the stack after backup: %v", startCommand, err))
				s.logger.Error("failed to restart stack after backup",
					zap.String("run_id", run.ID),
					zap.String("stack_name", run.StackName),
					zap.String("command", startCommand),
					zap.Error(err),
				)
			}
		}()
	}

	if err := s.prepareRepository(ctx, image, password, run, writer); err != nil {
		return err
	}

	for i := range run.Components {
		if err := s.backupComponent(ctx, image, password, run, &run.Components[i], writer); err != nil {
			return err
		}
		if err := s.persistence.PersistRun(run); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) verifyRepository(ctx context.Context, image, password string, run *Run, writer ProgressWriter) error {
	writer.WriteProgress("Verifying repository integrity...")
	check, err := s.runResticBuffered(ctx, image, run.StackName, run.ID, password, []string{"check"}, []mount.Mount{repoMount(s.repoHostPath(run.StackName), false)})
	if err != nil {
		return fmt.Errorf("failed to verify the backup repository: %w", err)
	}
	verified := check.exitCode == 0
	run.Verified = &verified
	if !verified {
		run.VerifyError = check.output
		return fmt.Errorf("the repository failed its integrity check after this backup (exit code %d): %s", check.exitCode, lastLine(check.output))
	}
	writer.WriteStdout("Repository integrity verified")

	stats, err := s.runResticBuffered(ctx, image, run.StackName, run.ID, password, []string{"stats", "--mode", "raw-data", "--json"}, []mount.Mount{repoMount(s.repoHostPath(run.StackName), false)})
	if err != nil || stats.exitCode != 0 {
		s.logger.Warn("failed to measure repository size after backup",
			zap.String("run_id", run.ID),
			zap.String("stack_name", run.StackName),
			zap.Error(err),
		)
		return nil
	}
	if size, ok := parseRepoStatsTotalSize(stats.output); ok {
		run.RepoSizeBytes = size
		writer.WriteStdout("All of this stack's backups now use " + formatBytes(size) + " on disk")
	}
	return nil
}

func parseRepoStatsTotalSize(output string) (uint64, bool) {
	for line := range strings.Lines(output) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var stats struct {
			TotalSize uint64 `json:"total_size"`
		}
		if err := json.Unmarshal([]byte(line), &stats); err == nil {
			return stats.TotalSize, true
		}
	}
	return 0, false
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func (s *Service) prepareRepository(ctx context.Context, image, password string, run *Run, writer ProgressWriter) error {
	repoPath := s.repoHostPath(run.StackName)

	version, err := s.runResticBuffered(ctx, image, run.StackName, run.ID, password, []string{"version"}, nil)
	if err != nil {
		return fmt.Errorf("failed to run restic in a helper container: %w", err)
	}
	if version.exitCode != 0 {
		return fmt.Errorf("restic version check failed with exit code %d: %s", version.exitCode, version.output)
	}
	run.ResticVersion = firstLine(version.output)
	writer.WriteStdout("Backup engine: " + run.ResticVersion)

	probe, err := s.runResticBuffered(ctx, image, run.StackName, run.ID, password, []string{"cat", "config"}, []mount.Mount{repoMount(repoPath, false)})
	if err != nil {
		return fmt.Errorf("failed to probe the backup repository: %w", err)
	}
	switch probe.exitCode {
	case 0:
	case resticExitRepoDoesNotExist:
		writer.WriteProgress("Initialising new backup repository for this stack...")
		initResult, err := s.runResticBuffered(ctx, image, run.StackName, run.ID, password, []string{"init"}, []mount.Mount{repoMount(repoPath, false)})
		if err != nil {
			return fmt.Errorf("failed to initialise the backup repository: %w", err)
		}
		if initResult.exitCode != 0 {
			return fmt.Errorf("repository initialisation failed with exit code %d: %s", initResult.exitCode, initResult.output)
		}
	case resticExitWrongPassword:
		return fmt.Errorf("the backup password configured for this server in berth does not open the existing repository for stack %q; refusing to continue (a new repository is never created next to one that cannot be read)", run.StackName)
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

func (s *Service) backupComponent(ctx context.Context, image, password string, run *Run, component *Component, writer ProgressWriter) error {
	writer.WriteProgress("Backing up " + component.ID + "...")

	sourceMount, err := componentSourceMount(*component)
	if err != nil {
		component.Error = err.Error()
		return err
	}
	mounts := []mount.Mount{repoMount(s.repoHostPath(run.StackName), false), sourceMount}

	parser := newResticOutputParser(component.ID, writer)
	args := backupArgs(*component, run.StackName, run.ID)

	exitCode, err := s.runResticStreaming(ctx, image, run.StackName, run.ID, password, args, mounts,
		parser.handleLine,
		writer.WriteStderr,
	)
	if err != nil {
		component.Error = err.Error()
		return fmt.Errorf("backup of %s failed: %w", component.ID, err)
	}
	if exitCode != 0 {
		message := fmt.Sprintf("backup of %s failed with restic exit code %d", component.ID, exitCode)
		if len(parser.errors) > 0 {
			message += ": " + strings.Join(parser.errors, "; ")
		}
		component.Error = message
		return fmt.Errorf("%s", message)
	}
	if parser.summary == nil {
		component.Error = "restic reported success but no snapshot summary was seen"
		return fmt.Errorf("backup of %s produced no snapshot summary; refusing to record it as successful", component.ID)
	}

	component.SnapshotID = parser.summary.SnapshotID
	component.FilesNew = parser.summary.FilesNew
	component.FilesChanged = parser.summary.FilesChanged
	component.FilesUnmodified = parser.summary.FilesUnmodified
	component.BytesAdded = parser.summary.DataAdded
	component.BytesProcessed = parser.summary.TotalBytesProcessed
	component.DurationSecs = parser.summary.TotalDuration

	writer.WriteStdout(fmt.Sprintf("%s: snapshot %s (%s added, %d new / %d changed / %d unmodified files)",
		component.ID, component.SnapshotID, formatBytes(component.BytesAdded),
		component.FilesNew, component.FilesChanged, component.FilesUnmodified))
	return nil
}

func (s *Service) composeComponents(stackName, stackPath string) ([]Component, []SkippedMount, error) {
	cmd, err := s.commandExec.ExecuteComposeCommand(stackName, "config", "--format", "json")
	if err != nil {
		return nil, nil, err
	}
	output, err := cmd.Output()
	if err != nil {
		detail := ""
		if exitErr, ok := err.(*exec.ExitError); ok {
			detail = ": " + strings.TrimSpace(string(exitErr.Stderr))
		}
		return nil, nil, fmt.Errorf("failed to read the stack's compose configuration: %w%s", err, detail)
	}

	project, err := parseComposeProject(output)
	if err != nil {
		return nil, nil, err
	}

	return BuildComponents(project, stackPath, s.cfg.BackupLocation)
}

func (s *Service) enumerateComponents(ctx context.Context, stackName, stackPath string) ([]Component, []SkippedMount, error) {
	components, skipped, err := s.composeComponents(stackName, stackPath)
	if err != nil {
		return nil, nil, err
	}

	components, skipped, err = s.resolveAnonymousVolumes(ctx, stackName, components, skipped)
	if err != nil {
		return nil, nil, err
	}

	s.enrichVolumeDefinitions(ctx, components)
	return components, skipped, nil
}

func (s *Service) enrichVolumeDefinitions(ctx context.Context, components []Component) {
	for i := range components {
		component := &components[i]
		if component.Kind != KindVolume || component.VolumeDef == nil || component.VolumeDef.External || component.VolumeName == "" {
			continue
		}
		vol, err := s.dockerClient.InspectVolume(ctx, component.VolumeName)
		if err != nil {
			continue
		}
		if vol.Driver != "" {
			component.VolumeDef.Driver = vol.Driver
		}
		if len(vol.Options) > 0 {
			component.VolumeDef.DriverOpts = vol.Options
		}
		if len(vol.Labels) > 0 {
			component.VolumeDef.Labels = vol.Labels
		}
	}
}

func (s *Service) resolveAnonymousVolumes(ctx context.Context, stackName string, components []Component, skipped []SkippedMount) ([]Component, []SkippedMount, error) {
	var resolved []Component
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
			return nil, nil, fmt.Errorf("failed to list containers for service %q while resolving anonymous volumes: %w", component.Service, err)
		}

		found := false
		for _, summary := range containers {
			info, err := s.dockerClient.ContainerInspect(ctx, summary.ID)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to inspect container %s while resolving anonymous volumes: %w", summary.ID, err)
			}
			for _, containerMount := range info.Mounts {
				if string(containerMount.Type) != "volume" || containerMount.Destination != component.Target || containerMount.Name == "" {
					continue
				}
				instance := component
				instance.VolumeName = containerMount.Name
				if number := info.Config.Labels[composeContainerNumberLabel]; number != "" && len(containers) > 1 {
					instance.ID = component.ID + ":" + number
					instance.ContainerNumber = number
				}
				resolved = append(resolved, instance)
				found = true
			}
		}

		if !found {
			skipped = append(skipped, SkippedMount{
				Kind:    string(KindAnonymousVolume),
				Service: component.Service,
				Target:  component.Target,
				Reason:  "no container exists for this service yet, so its anonymous volume has not been created",
			})
		}
	}
	return resolved, skipped, nil
}

func (s *Service) precheckSources(ctx context.Context, image string, run *Run) error {
	var hostPaths []string
	for _, component := range run.Components {
		if component.Kind == KindStackDirectory || component.Kind == KindBindMount {
			hostPaths = append(hostPaths, component.SourcePath)
		}
	}

	missing, err := s.probeMissingHostPaths(ctx, image, run.StackName, run.ID, hostPaths)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("refusing to back up: the following mount source paths do not exist on the host: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (s *Service) runComposeLifecycle(ctx context.Context, stackPath, command string, writer ProgressWriter) error {
	cmd := exec.CommandContext(ctx, "docker", "compose", command)
	cmd.Dir = stackPath
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/tmp",
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan struct{}, 2)
	go func() {
		streamLines(stdout, writer.WriteStdout)
		done <- struct{}{}
	}()
	go func() {
		streamLines(stderr, writer.WriteStderr)
		done <- struct{}{}
	}()
	<-done
	<-done

	return cmd.Wait()
}

func firstLine(s string) string {
	if index := strings.IndexByte(s, '\n'); index >= 0 {
		return strings.TrimSpace(s[:index])
	}
	return strings.TrimSpace(s)
}
