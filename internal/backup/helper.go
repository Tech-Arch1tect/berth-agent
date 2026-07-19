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

func (s *Service) runResticStreaming(ctx context.Context, image, stackName, runID, password string, args []string, mounts []mount.Mount, stdoutLine, stderrLine func(string)) (int, error) {
	if password == "" {
		return 0, fmt.Errorf("no backup password was provided for this operation; backups must be enabled and given an encryption password in this server's settings in berth")
	}

	spec := docker.ContainerRunSpec{
		Image:      image,
		Entrypoint: []string{"restic"},
		Cmd:        args,
		Env:        resticEnv(password),
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

func (s *Service) runResticBuffered(ctx context.Context, image, stackName, runID, password string, args []string, mounts []mount.Mount) (bufferedResticResult, error) {
	var buffer bytes.Buffer
	appendLine := func(line string) {
		buffer.WriteString(line)
		buffer.WriteByte('\n')
	}
	exitCode, err := s.runResticStreaming(ctx, image, stackName, runID, password, args, mounts, appendLine, appendLine)
	return bufferedResticResult{exitCode: exitCode, output: strings.TrimSpace(buffer.String())}, err
}

type hostPathProbe struct {
	Type     string
	Resolved string
}

func (s *Service) probeHostPaths(ctx context.Context, image, stackName, runID string, paths []string) (map[string]hostPathProbe, error) {
	if len(paths) == 0 {
		return map[string]hostPathProbe{}, nil
	}

	script := `for p in "$@"; do
  fp="` + helperHostProbeRoot + `$p"
  if [ ! -e "$fp" ]; then t=missing
  elif [ -f "$fp" ]; then t=file
  elif [ -d "$fp" ]; then t=dir
  else t=other; fi
  rp=$(chroot "` + helperHostProbeRoot + `" readlink -f "$p" 2>/dev/null || true)
  printf 'BERTH_NODE\n%s\n%s\n%s\n' "$p" "$t" "$rp"
done`

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
		return nil, fmt.Errorf("failed to check the backup source paths on the host: %w", err)
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("host path check failed with exit code %d: %s", exitCode, strings.TrimSpace(buffer.String()))
	}

	return parseHostPathTypes(buffer.String()), nil
}

func parseHostPathTypes(output string) map[string]hostPathProbe {
	probes := map[string]hostPathProbe{}
	lines := strings.Split(output, "\n")
	for i := 0; i < len(lines); i++ {
		if lines[i] != "BERTH_NODE" || i+3 >= len(lines) {
			continue
		}
		path, kind, resolved := lines[i+1], lines[i+2], lines[i+3]
		probes[path] = hostPathProbe{Type: kind, Resolved: resolved}
		i += 3
	}
	return probes
}

func commandEcho(name string, args []string) string {
	return "Running: " + name + " " + strings.Join(args, " ")
}

func streamLines(reader io.Reader, handle func(string)) {
	scanner := newLargeScanner(reader)
	for scanner.Scan() {
		handle(scanner.Text())
	}
}
