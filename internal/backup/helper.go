package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/tech-arch1tect/berth-agent/internal/docker"
)

const helperHostProbeRoot = "/berth-backup/host"

func (s *Service) helperImage(ctx context.Context) (string, error) {
	if s.cfg.BackupHelperImage != "" {
		return s.cfg.BackupHelperImage, nil
	}

	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("could not read the agent's hostname to discover its container image: %w; set BACKUP_HELPER_IMAGE explicitly", err)
	}

	info, err := s.dockerClient.ContainerInspect(ctx, hostname)
	if err != nil {
		return "", fmt.Errorf("could not inspect the agent's own container %q to discover its image: %w; set BACKUP_HELPER_IMAGE explicitly", hostname, err)
	}
	return info.Image, nil
}

func (s *Service) repoHostPath(stackName string) string {
	return filepath.Join(s.cfg.BackupLocation, stackName)
}

func repoMount(repoHostPath string, readOnly bool) mount.Mount {
	return mount.Mount{
		Type:     mount.TypeBind,
		Source:   repoHostPath,
		Target:   helperRepoPath,
		ReadOnly: readOnly,
		BindOptions: &mount.BindOptions{
			CreateMountpoint: true,
		},
	}
}

func componentSourceMount(c Component) (mount.Mount, error) {
	target := componentSourceMountPath(c)
	switch c.Kind {
	case KindStackDirectory, KindBindMount:
		return mount.Mount{Type: mount.TypeBind, Source: c.SourcePath, Target: target, ReadOnly: true}, nil
	case KindVolume, KindAnonymousVolume:
		if c.VolumeName == "" {
			return mount.Mount{}, fmt.Errorf("component %s has no resolved volume name", c.ID)
		}
		return mount.Mount{Type: mount.TypeVolume, Source: c.VolumeName, Target: target, ReadOnly: true}, nil
	default:
		return mount.Mount{}, fmt.Errorf("component %s has unsupported kind %s", c.ID, c.Kind)
	}
}

func (s *Service) helperLabels(stackName, runID string) map[string]string {
	return map[string]string{
		"berth.backup.stack": stackName,
		"berth.backup.run":   runID,
	}
}

func (s *Service) runResticStreaming(ctx context.Context, image, stackName, runID string, args []string, mounts []mount.Mount, stdoutLine, stderrLine func(string)) (int, error) {
	spec := docker.ContainerRunSpec{
		Image:      image,
		Entrypoint: []string{"restic"},
		Cmd:        args,
		Env:        resticEnv(s.cfg.BackupPassword),
		Mounts:     mounts,
		Labels:     s.helperLabels(stackName, runID),
	}

	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	done := make(chan struct{}, 2)
	go func() {
		streamLines(stdoutReader, stdoutLine)
		done <- struct{}{}
	}()
	go func() {
		streamLines(stderrReader, stderrLine)
		done <- struct{}{}
	}()

	exitCode, err := s.dockerClient.RunContainer(ctx, spec, stdoutWriter, stderrWriter)
	stdoutWriter.Close()
	stderrWriter.Close()
	<-done
	<-done

	return exitCode, err
}

type bufferedResticResult struct {
	exitCode int
	output   string
}

func (s *Service) runResticBuffered(ctx context.Context, image, stackName, runID string, args []string, mounts []mount.Mount) (bufferedResticResult, error) {
	var buffer bytes.Buffer
	appendLine := func(line string) {
		buffer.WriteString(line)
		buffer.WriteByte('\n')
	}
	exitCode, err := s.runResticStreaming(ctx, image, stackName, runID, args, mounts, appendLine, appendLine)
	return bufferedResticResult{exitCode: exitCode, output: strings.TrimSpace(buffer.String())}, err
}

func (s *Service) probeMissingHostPaths(ctx context.Context, image, stackName, runID string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	script := `missing=0
for p in "$@"; do
  if [ ! -e "` + helperHostProbeRoot + `$p" ]; then
    echo "BERTH_MISSING:$p"
    missing=1
  fi
done
exit $missing`

	spec := docker.ContainerRunSpec{
		Image:      image,
		Entrypoint: append([]string{"/bin/sh", "-c", script, "sh"}, paths...),
		Env:        []string{},
		Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: "/", Target: helperHostProbeRoot, ReadOnly: true},
		},
		Labels: s.helperLabels(stackName, runID),
	}

	var buffer bytes.Buffer
	exitCode, err := s.dockerClient.RunContainer(ctx, spec, &buffer, &buffer)
	if err != nil {
		return nil, fmt.Errorf("failed to check that all backup source paths exist on the host: %w", err)
	}
	if exitCode != 0 && exitCode != 1 {
		return nil, fmt.Errorf("host path check failed with exit code %d: %s", exitCode, strings.TrimSpace(buffer.String()))
	}

	var missing []string
	for line := range strings.Lines(buffer.String()) {
		if path, found := strings.CutPrefix(strings.TrimSpace(line), "BERTH_MISSING:"); found {
			missing = append(missing, path)
		}
	}
	return missing, nil
}

func streamLines(reader io.Reader, handle func(string)) {
	scanner := newLargeScanner(reader)
	for scanner.Scan() {
		handle(scanner.Text())
	}
}
