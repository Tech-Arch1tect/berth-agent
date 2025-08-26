package stats

import (
	"berth-agent/config"
	"berth-agent/internal/docker"
	"berth-agent/internal/stack"
	"berth-agent/internal/validation"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/docker/docker/api/types/container"
)

type Service struct {
	stackLocation string
	dockerClient  *docker.Client
	stackService  *stack.Service
}

func NewService(cfg *config.Config, dockerClient *docker.Client, stackService *stack.Service) *Service {
	return &Service{
		stackLocation: cfg.StackLocation,
		dockerClient:  dockerClient,
		stackService:  stackService,
	}
}

func (s *Service) GetStackStats(name string) (*StackStats, error) {
	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack '%s' not found", name)
	}

	containers, err := s.stackService.GetContainerInfo(name)
	if err != nil {
		return nil, fmt.Errorf("failed to get container info: %w", err)
	}

	stats := &StackStats{
		StackName:  name,
		Containers: make([]ContainerStats, 0),
	}

	for serviceName, containerList := range containers {
		for _, container := range containerList {
			if container.State != "running" {
				continue
			}

			containerStats, err := s.getContainerStats(container.Name, serviceName)
			if err != nil {
				continue
			}

			stats.Containers = append(stats.Containers, *containerStats)
		}
	}

	return stats, nil
}

func (s *Service) getContainerStats(containerName, serviceName string) (*ContainerStats, error) {
	ctx := context.Background()
	containerJSON, err := s.dockerClient.ContainerInspect(ctx, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	statsReader, err := s.dockerClient.ContainerStats(ctx, containerJSON.ID, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get container stats: %w", err)
	}
	defer statsReader.Body.Close()

	var stats container.StatsResponse
	if err := json.NewDecoder(statsReader.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("failed to decode stats: %w", err)
	}

	cpuPercent := s.calculateCPUPercent(&stats)
	memoryUsage := stats.MemoryStats.Usage
	memoryLimit := stats.MemoryStats.Limit
	memoryPercent := float64(memoryUsage) / float64(memoryLimit) * 100.0

	return &ContainerStats{
		Name:          containerName,
		ServiceName:   serviceName,
		CPUPercent:    cpuPercent,
		MemoryUsage:   memoryUsage,
		MemoryLimit:   memoryLimit,
		MemoryPercent: memoryPercent,
	}, nil
}

func (s *Service) calculateCPUPercent(stats *container.StatsResponse) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)

	if systemDelta > 0.0 && cpuDelta > 0.0 {
		return (cpuDelta / systemDelta) * float64(len(stats.CPUStats.CPUUsage.PercpuUsage)) * 100.0
	}
	return 0.0
}
