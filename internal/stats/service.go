package stats

import (
	"berth-agent/config"
	"berth-agent/internal/docker"
	"berth-agent/internal/logging"
	"berth-agent/internal/stack"
	"berth-agent/internal/validation"
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type cpuCacheEntry struct {
	userUsec     uint64
	systemUsec   uint64
	timestamp    time.Time
	lastAccessed time.Time
}

type Service struct {
	stackLocation string
	dockerClient  *docker.Client
	stackService  *stack.Service
	cgroupRoot    string
	cpuCache      map[string]*cpuCacheEntry
	cacheMutex    sync.RWMutex
	logger        *logging.Logger
}

func NewService(cfg *config.Config, dockerClient *docker.Client, stackService *stack.Service, logger *logging.Logger) *Service {
	service := &Service{
		stackLocation: cfg.StackLocation,
		dockerClient:  dockerClient,
		stackService:  stackService,
		cgroupRoot:    "/sys/fs/cgroup",
		cpuCache:      make(map[string]*cpuCacheEntry),
		logger:        logger.With(zap.String("component", "stats")),
	}

	logger.Info("Stats service initialized",
		zap.String("stack_location", cfg.StackLocation),
		zap.String("cgroup_root", service.cgroupRoot),
	)

	go service.cleanupCacheLoop()

	return service
}

func (s *Service) GetStackStats(name string) (*StackStats, error) {
	s.logger.Info("Collecting stack stats", zap.String("stack", name))

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		s.logger.Error("Invalid stack name", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		s.logger.Error("Stack not found", zap.String("stack", name), zap.String("path", stackPath))
		return nil, fmt.Errorf("stack '%s' not found", name)
	}

	containers, err := s.stackService.GetContainerInfo(name)
	if err != nil {
		s.logger.Error("Failed to get container info", zap.String("stack", name), zap.Error(err))
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
		s.logger.Debug("No running containers found", zap.String("stack", name))
		return stats, nil
	}

	s.logger.Debug("Found running containers",
		zap.String("stack", name),
		zap.Int("count", len(runningContainers)),
	)

	ctx := context.Background()
	for i, job := range runningContainers {
		s.logger.Debug("Inspecting container",
			zap.String("container", job.name),
			zap.String("id", job.id),
		)
		containerInfo, err := s.dockerClient.ContainerInspect(ctx, job.id)
		if err == nil && containerInfo.State.Pid != 0 {
			runningContainers[i].pid = containerInfo.State.Pid
		} else if err != nil {
			s.logger.Debug("Failed to inspect container",
				zap.String("container", job.name),
				zap.Error(err),
			)
		}
	}

	statsChan := make(chan ContainerStats, len(runningContainers))
	var wg sync.WaitGroup

	for _, job := range runningContainers {
		wg.Add(1)
		go func(containerName, serviceName, containerID string, pid int) {
			defer wg.Done()

			s.logger.Debug("Collecting container stats",
				zap.String("container", containerName),
				zap.String("service", serviceName),
			)

			containerStats, err := s.getContainerStatsFromCgroups(containerName, serviceName, containerID, pid)
			if err != nil {
				s.logger.Error("Failed to collect container stats",
					zap.String("container", containerName),
					zap.Error(err),
				)
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

	s.logger.Info("Stack stats collected successfully",
		zap.String("stack", name),
		zap.Int("containers", len(stats.Containers)),
	)

	return stats, nil
}

func (s *Service) getContainerID(containerName string) (string, error) {
	s.logger.Debug("Looking up container ID", zap.String("container", containerName))

	ctx := context.Background()
	containers, err := s.dockerClient.ContainerList(ctx, nil)
	if err != nil {
		s.logger.Error("Failed to list containers via Docker API",
			zap.String("container", containerName),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	for _, container := range containers {
		for _, name := range container.Names {
			if strings.TrimPrefix(name, "/") == containerName {
				s.logger.Debug("Container ID found",
					zap.String("container", containerName),
					zap.String("id", container.ID),
				)
				return container.ID, nil
			}
		}
	}

	s.logger.Debug("Container not found in Docker API",
		zap.String("container", containerName),
	)
	return "", fmt.Errorf("container %s not found", containerName)
}

func (s *Service) getContainerStatsFromCgroups(containerName, serviceName, containerID string, pid int) (*ContainerStats, error) {
	s.logger.Debug("Collecting stats from cgroups",
		zap.String("container", containerName),
		zap.String("id", containerID),
		zap.Int("pid", pid),
	)

	stats := &ContainerStats{
		Name:        containerName,
		ServiceName: serviceName,
	}

	cgroupPath := s.getContainerCgroupPath(containerID)
	s.logger.Debug("Using cgroup path",
		zap.String("container", containerName),
		zap.String("path", cgroupPath),
	)

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
		s.logger.Debug("Memory stats collected",
			zap.String("container", containerName),
			zap.Uint64("usage", memoryStats.Usage),
			zap.Float64("percent", memoryStats.Percent),
		)
	} else {
		s.logger.Debug("Failed to collect memory stats",
			zap.String("container", containerName),
			zap.Error(err),
		)
	}

	cpuStats, err := s.getCPUStats(cgroupPath)
	if err == nil {
		stats.CPUUserTime = cpuStats.UserTime
		stats.CPUSystemTime = cpuStats.SystemTime
		s.logger.Debug("CPU stats collected",
			zap.String("container", containerName),
			zap.Uint64("user_time", cpuStats.UserTime),
			zap.Uint64("system_time", cpuStats.SystemTime),
		)
	} else {
		s.logger.Debug("Failed to collect CPU stats",
			zap.String("container", containerName),
			zap.Error(err),
		)
	}

	stats.CPUPercent = s.calculateCPUPercentFromCgroup(cgroupPath)

	blkioStats, err := s.getBlockIOStats(cgroupPath)
	if err == nil {
		stats.BlockReadBytes = blkioStats.ReadBytes
		stats.BlockWriteBytes = blkioStats.WriteBytes
		stats.BlockReadOps = blkioStats.ReadOps
		stats.BlockWriteOps = blkioStats.WriteOps
		s.logger.Debug("Block I/O stats collected",
			zap.String("container", containerName),
			zap.Uint64("read_bytes", blkioStats.ReadBytes),
			zap.Uint64("write_bytes", blkioStats.WriteBytes),
		)
	} else {
		s.logger.Debug("Failed to collect block I/O stats",
			zap.String("container", containerName),
			zap.Error(err),
		)
	}

	networkStats, err := s.getNetworkStats(pid)
	if err == nil {
		stats.NetworkRxBytes = networkStats.RxBytes
		stats.NetworkTxBytes = networkStats.TxBytes
		stats.NetworkRxPackets = networkStats.RxPackets
		stats.NetworkTxPackets = networkStats.TxPackets
		s.logger.Debug("Network stats collected",
			zap.String("container", containerName),
			zap.Uint64("rx_bytes", networkStats.RxBytes),
			zap.Uint64("tx_bytes", networkStats.TxBytes),
		)
	} else {
		s.logger.Debug("Failed to collect network stats",
			zap.String("container", containerName),
			zap.Error(err),
		)
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

	percent := 0.0

	return &cpuStats{
		Percent:    percent,
		UserTime:   cpuStat["user_usec"] / 1000,
		SystemTime: cpuStat["system_usec"] / 1000,
	}, nil
}

func (s *Service) cleanupCacheLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanupStaleCache()
	}
}

func (s *Service) cleanupStaleCache() {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	initialSize := len(s.cpuCache)
	removed := 0

	for cacheKey, entry := range s.cpuCache {
		if entry.lastAccessed.Before(cutoff) {
			delete(s.cpuCache, cacheKey)
			removed++
		}
	}

	if removed > 0 {
		s.logger.Debug("Cleaned up stale cache entries",
			zap.Int("removed", removed),
			zap.Int("initial_size", initialSize),
			zap.Int("final_size", len(s.cpuCache)),
		)
	}
}

func (s *Service) calculateCPUPercentFromCgroup(cgroupPath string) float64 {
	cpuStatPath := filepath.Join(cgroupPath, "cpu.stat")

	cpuStat, err := s.parseCPUStat(cpuStatPath)
	if err != nil {
		return 0.0
	}

	userUsec := cpuStat["user_usec"]
	systemUsec := cpuStat["system_usec"]

	now := time.Now()

	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	cacheKey := cgroupPath
	if cached, exists := s.cpuCache[cacheKey]; exists {
		s.logger.Debug("CPU cache hit",
			zap.String("cgroup", cgroupPath),
			zap.Time("cached_at", cached.timestamp),
		)
		cached.lastAccessed = now

		deltaTime := now.Sub(cached.timestamp).Seconds()

		if deltaTime == 0 {
			return -1.0
		}

		deltaUserUsec := float64(userUsec - cached.userUsec)
		deltaSystemUsec := float64(systemUsec - cached.systemUsec)
		totalDeltaUsec := deltaUserUsec + deltaSystemUsec

		deltaCPUSeconds := totalDeltaUsec / 1_000_000.0

		numCores := float64(runtime.NumCPU())
		maxPossibleCPUTime := deltaTime * numCores

		cpuPercent := (deltaCPUSeconds / maxPossibleCPUTime) * 100.0

		if cpuPercent > 100.0 {
			cpuPercent = 100.0
		}

		s.logger.Debug("CPU percentage calculated",
			zap.String("cgroup", cgroupPath),
			zap.Float64("percent", cpuPercent),
			zap.Float64("delta_time", deltaTime),
		)

		cached.userUsec = userUsec
		cached.systemUsec = systemUsec
		cached.timestamp = now

		return cpuPercent
	}

	s.logger.Debug("CPU cache miss - initializing",
		zap.String("cgroup", cgroupPath),
	)

	s.cpuCache[cacheKey] = &cpuCacheEntry{
		userUsec:     userUsec,
		systemUsec:   systemUsec,
		timestamp:    now,
		lastAccessed: now,
	}

	return -1.0
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
