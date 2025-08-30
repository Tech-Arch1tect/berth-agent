package stats

import (
	"berth-agent/config"
	"berth-agent/internal/docker"
	"berth-agent/internal/stack"
	"berth-agent/internal/validation"
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Service struct {
	stackLocation string
	dockerClient  *docker.Client
	stackService  *stack.Service
	cgroupRoot    string
}

func NewService(cfg *config.Config, dockerClient *docker.Client, stackService *stack.Service) *Service {
	return &Service{
		stackLocation: cfg.StackLocation,
		dockerClient:  dockerClient,
		stackService:  stackService,
		cgroupRoot:    "/sys/fs/cgroup",
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
		id          string
		pid         int
	}

	var runningContainers []containerJob

	for serviceName, containerList := range containers {
		for _, container := range containerList {
			if container.State == "running" {
				containerID, err := s.getContainerID(container.Name)
				if err != nil {
					continue
				}
				runningContainers = append(runningContainers, containerJob{
					name:        container.Name,
					serviceName: serviceName,
					id:          containerID,
				})
			}
		}
	}

	if len(runningContainers) == 0 {
		return stats, nil
	}

	ctx := context.Background()
	for i, job := range runningContainers {
		containerInfo, err := s.dockerClient.ContainerInspect(ctx, job.id)
		if err == nil && containerInfo.State.Pid != 0 {
			runningContainers[i].pid = containerInfo.State.Pid
		}
	}

	statsChan := make(chan ContainerStats, len(runningContainers))
	var wg sync.WaitGroup

	for _, job := range runningContainers {
		wg.Add(1)
		go func(containerName, serviceName, containerID string, pid int) {
			defer wg.Done()

			containerStats, err := s.getContainerStatsFromCgroups(containerName, serviceName, containerID, pid)
			if err != nil {
				return
			}

			statsChan <- *containerStats
		}(job.name, job.serviceName, job.id, job.pid)
	}

	go func() {
		wg.Wait()
		close(statsChan)
	}()

	for containerStats := range statsChan {
		stats.Containers = append(stats.Containers, containerStats)
	}

	sort.Slice(stats.Containers, func(i, j int) bool {
		return stats.Containers[i].Name < stats.Containers[j].Name
	})

	return stats, nil
}

func (s *Service) getContainerID(containerName string) (string, error) {
	ctx := context.Background()
	containers, err := s.dockerClient.ContainerList(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	for _, container := range containers {
		for _, name := range container.Names {
			if strings.TrimPrefix(name, "/") == containerName {
				return container.ID, nil
			}
		}
	}

	return "", fmt.Errorf("container %s not found", containerName)
}

func (s *Service) getContainerStatsFromCgroups(containerName, serviceName, containerID string, pid int) (*ContainerStats, error) {
	stats := &ContainerStats{
		Name:        containerName,
		ServiceName: serviceName,
	}

	cgroupPath := s.getContainerCgroupPath(containerID)

	memoryStats, err := s.getMemoryStats(cgroupPath)
	if err == nil {
		stats.MemoryUsage = memoryStats.Usage
		stats.MemoryLimit = memoryStats.Limit
		stats.MemoryPercent = memoryStats.Percent
		stats.MemoryRSS = memoryStats.RSS
		stats.MemoryCache = memoryStats.Cache
		stats.MemorySwap = memoryStats.Swap
		stats.PageFaults = memoryStats.PageFaults
		stats.PageMajorFaults = memoryStats.PageMajorFaults
	}

	cpuStats, err := s.getCPUStats(cgroupPath)
	if err == nil {
		stats.CPUPercent = cpuStats.Percent
		stats.CPUUserTime = cpuStats.UserTime
		stats.CPUSystemTime = cpuStats.SystemTime
	}

	blkioStats, err := s.getBlockIOStats(cgroupPath)
	if err == nil {
		stats.BlockReadBytes = blkioStats.ReadBytes
		stats.BlockWriteBytes = blkioStats.WriteBytes
		stats.BlockReadOps = blkioStats.ReadOps
		stats.BlockWriteOps = blkioStats.WriteOps
	}

	networkStats, err := s.getNetworkStats(pid)
	if err == nil {
		stats.NetworkRxBytes = networkStats.RxBytes
		stats.NetworkTxBytes = networkStats.TxBytes
		stats.NetworkRxPackets = networkStats.RxPackets
		stats.NetworkTxPackets = networkStats.TxPackets
	}

	return stats, nil
}

func (s *Service) getContainerCgroupPath(containerID string) string {
	systemdPath := filepath.Join(s.cgroupRoot, "system.slice", fmt.Sprintf("docker-%s.scope", containerID))
	if _, err := os.Stat(systemdPath); err == nil {
		return systemdPath
	}
	return filepath.Join(s.cgroupRoot, "docker", containerID)
}

type memoryStats struct {
	Usage           uint64
	Limit           uint64
	Percent         float64
	RSS             uint64
	Cache           uint64
	Swap            uint64
	PageFaults      uint64
	PageMajorFaults uint64
}

type cpuStats struct {
	Percent    float64
	UserTime   uint64
	SystemTime uint64
}

type blkioStats struct {
	ReadBytes  uint64
	WriteBytes uint64
	ReadOps    uint64
	WriteOps   uint64
}

type networkStats struct {
	RxBytes   uint64
	TxBytes   uint64
	RxPackets uint64
	TxPackets uint64
}

func (s *Service) getMemoryStats(cgroupPath string) (*memoryStats, error) {
	memoryUsagePath := filepath.Join(cgroupPath, "memory.current")
	memoryLimitPath := filepath.Join(cgroupPath, "memory.max")
	memoryStatPath := filepath.Join(cgroupPath, "memory.stat")

	usage, err := s.readUint64FromFile(memoryUsagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read memory usage: %w", err)
	}

	limit, err := s.readUint64FromFile(memoryLimitPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read memory limit: %w", err)
	}

	memStat, err := s.parseMemoryStat(memoryStatPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse memory.stat: %w", err)
	}

	percent := 0.0
	if limit > 0 && limit != 9223372036854775807 {
		percent = float64(usage) / float64(limit) * 100.0
	}

	return &memoryStats{
		Usage:           usage,
		Limit:           limit,
		Percent:         percent,
		RSS:             memStat["anon"],
		Cache:           memStat["file"],
		Swap:            memStat["swap"],
		PageFaults:      memStat["pgfault"],
		PageMajorFaults: memStat["pgmajfault"],
	}, nil
}

func (s *Service) getCPUStats(cgroupPath string) (*cpuStats, error) {
	cpuStatPath := filepath.Join(cgroupPath, "cpu.stat")

	cpuStat, err := s.parseCPUStat(cpuStatPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cpu.stat: %w", err)
	}

	return &cpuStats{
		Percent:    0.0,
		UserTime:   cpuStat["user_usec"] / 1000,
		SystemTime: cpuStat["system_usec"] / 1000,
	}, nil
}

func (s *Service) getBlockIOStats(cgroupPath string) (*blkioStats, error) {
	ioStatPath := filepath.Join(cgroupPath, "io.stat")

	ioStat, err := s.parseIOStat(ioStatPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse io.stat: %w", err)
	}

	return &blkioStats{
		ReadBytes:  ioStat["rbytes"],
		WriteBytes: ioStat["wbytes"],
		ReadOps:    ioStat["rios"],
		WriteOps:   ioStat["wios"],
	}, nil
}

func (s *Service) getNetworkStats(pid int) (*networkStats, error) {
	if pid == 0 {
		return &networkStats{}, nil
	}

	procNetDevPath := fmt.Sprintf("/proc/%d/net/dev", pid)

	file, err := os.Open(procNetDevPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", procNetDevPath, err)
	}
	defer file.Close()

	var totalRxBytes, totalTxBytes, totalRxPackets, totalTxPackets uint64

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, ":") && !strings.HasPrefix(line, "Inter-") && !strings.HasPrefix(line, " face") {
			parts := strings.Fields(line)
			if len(parts) >= 10 {
				rxBytes, _ := strconv.ParseUint(parts[1], 10, 64)
				rxPackets, _ := strconv.ParseUint(parts[2], 10, 64)
				txBytes, _ := strconv.ParseUint(parts[9], 10, 64)
				txPackets, _ := strconv.ParseUint(parts[10], 10, 64)

				totalRxBytes += rxBytes
				totalRxPackets += rxPackets
				totalTxBytes += txBytes
				totalTxPackets += txPackets
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan %s: %w", procNetDevPath, err)
	}

	return &networkStats{
		RxBytes:   totalRxBytes,
		TxBytes:   totalTxBytes,
		RxPackets: totalRxPackets,
		TxPackets: totalTxPackets,
	}, nil
}

func (s *Service) readUint64FromFile(filePath string) (uint64, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}

	content := strings.TrimSpace(string(data))
	if content == "max" {
		return 9223372036854775807, nil
	}

	value, err := strconv.ParseUint(content, 10, 64)
	if err != nil {
		return 0, err
	}

	return value, nil
}

func (s *Service) parseMemoryStat(filePath string) (map[string]uint64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	stats := make(map[string]uint64)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}

		stats[key] = value
	}

	return stats, scanner.Err()
}

func (s *Service) parseCPUStat(filePath string) (map[string]uint64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	stats := make(map[string]uint64)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}

		stats[key] = value
	}

	return stats, scanner.Err()
}

func (s *Service) parseIOStat(filePath string) (map[string]uint64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	stats := make(map[string]uint64)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		for i := 1; i < len(parts); i++ {
			kvPair := strings.Split(parts[i], "=")
			if len(kvPair) != 2 {
				continue
			}

			key := kvPair[0]
			value, err := strconv.ParseUint(kvPair[1], 10, 64)
			if err != nil {
				continue
			}

			if _, exists := stats[key]; !exists {
				stats[key] = 0
			}
			stats[key] += value
		}
	}

	return stats, scanner.Err()
}
