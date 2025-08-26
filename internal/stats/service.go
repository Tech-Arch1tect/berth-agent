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
	"sync"
	"time"

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

	type containerJob struct {
		name        string
		serviceName string
	}

	var runningContainers []containerJob
	for serviceName, containerList := range containers {
		for _, container := range containerList {
			if container.State == "running" {
				runningContainers = append(runningContainers, containerJob{
					name:        container.Name,
					serviceName: serviceName,
				})
			}
		}
	}

	if len(runningContainers) == 0 {
		return stats, nil
	}

	statsChan := make(chan ContainerStats, len(runningContainers))
	var wg sync.WaitGroup

	for _, job := range runningContainers {
		wg.Add(1)
		go func(containerName, serviceName string) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			containerStats, err := s.getContainerStatsWithContext(ctx, containerName, serviceName)
			if err != nil {
				return
			}

			statsChan <- *containerStats
		}(job.name, job.serviceName)
	}

	go func() {
		wg.Wait()
		close(statsChan)
	}()

	for containerStats := range statsChan {
		stats.Containers = append(stats.Containers, containerStats)
	}

	return stats, nil
}

func (s *Service) getContainerStats(containerName, serviceName string) (*ContainerStats, error) {
	return s.getContainerStatsWithContext(context.Background(), containerName, serviceName)
}

func (s *Service) getContainerStatsWithContext(ctx context.Context, containerName, serviceName string) (*ContainerStats, error) {
	statsReader, err := s.dockerClient.ContainerStats(ctx, containerName, false)
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
