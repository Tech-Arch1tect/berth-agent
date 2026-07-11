package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/pkg/stdcopy"
	"go.uber.org/zap"
)

type ContainerRunSpec struct {
	Image      string
	Entrypoint []string
	Cmd        []string
	Env        []string
	Mounts     []mount.Mount
	Labels     map[string]string
	WorkingDir string
}

func (c *Client) RunContainer(ctx context.Context, spec ContainerRunSpec, stdout, stderr io.Writer) (int, error) {
	created, err := c.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      spec.Image,
			Entrypoint: spec.Entrypoint,
			Cmd:        spec.Cmd,
			Env:        spec.Env,
			Labels:     spec.Labels,
			WorkingDir: spec.WorkingDir,
		},
		&container.HostConfig{
			Mounts: spec.Mounts,
		},
		nil, nil, "")
	if err != nil {
		return -1, fmt.Errorf("failed to create container from image %s: %w", spec.Image, err)
	}
	containerID := created.ID

	defer func() {
		removeCtx := context.WithoutCancel(ctx)
		if err := c.cli.ContainerRemove(removeCtx, containerID, container.RemoveOptions{Force: true}); err != nil {
			c.logger.Warn("failed to remove finished container",
				zap.String("container_id", containerID),
				zap.Error(err),
			)
		}
	}()

	attached, err := c.cli.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return -1, fmt.Errorf("failed to attach to container %s: %w", containerID, err)
	}
	defer attached.Close()

	if err := c.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return -1, fmt.Errorf("failed to start container %s: %w", containerID, err)
	}

	copyDone := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdout, stderr, attached.Reader)
		copyDone <- err
	}()

	waitCh, waitErrCh := c.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-waitErrCh:
		return -1, fmt.Errorf("failed to wait for container %s: %w", containerID, err)
	case result := <-waitCh:
		<-copyDone
		if result.Error != nil {
			return int(result.StatusCode), fmt.Errorf("container %s finished with error: %s", containerID, result.Error.Message)
		}
		return int(result.StatusCode), nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}
