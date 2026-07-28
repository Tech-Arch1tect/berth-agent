package stats

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tech-arch1tect/berth-agent/config"
	"github.com/tech-arch1tect/berth-agent/internal/docker"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"github.com/tech-arch1tect/berth-agent/internal/validation"

	"github.com/docker/docker/api/types/container"
	"go.uber.org/zap"
)

const (
	cacheTTL        = 900 * time.Millisecond
	maxSampleWindow = 30 * time.Second
	sampleRetention = 5 * time.Minute
	collectTimeout  = 10 * time.Second
)

type containerLister interface {
	ContainerList(ctx context.Context, filterLabels map[string][]string) ([]container.Summary, error)
}

type Service struct {
	stackLocation string
	containers    containerLister
	cgroupRoot    string
	procRoot      string
	logger        *logging.Logger

	mu       sync.Mutex
	cache    map[string]cacheEntry
	previous map[string]stackSample
	inflight map[string]*collection
}

type cacheEntry struct {
	stats *StackStats
	at    time.Time
}

type collection struct {
	done  chan struct{}
	stats *StackStats
	err   error
}

type containerSample struct {
	name        string
	serviceName string
	state       string
	collected   bool

	memoryCurrent      uint64
	memoryAnon         uint64
	memoryFile         uint64
	memoryInactiveFile uint64
	memorySwap         uint64
	memoryPeak         uint64
	memoryLimit        uint64
	oomKills           uint64
	memoryLimitHits    uint64
	pageFaults         uint64
	pageMajorFaults    uint64

	cpuUsageUsec     uint64
	cpuUserUsec      uint64
	cpuSystemUsec    uint64
	cpuThrottledUsec uint64
	cpuPeriods       uint64
	cpuThrottled     uint64
	cpuQuotaCores    float64

	networkRxBytes   uint64
	networkTxBytes   uint64
	networkRxPackets uint64
	networkTxPackets uint64

	blockReadBytes  uint64
	blockWriteBytes uint64
	blockReadOps    uint64
	blockWriteOps   uint64
}

type stackSample struct {
	at         time.Time
	containers map[string]containerSample
}

func NewService(cfg *config.Config, dockerClient *docker.Client, logger *logging.Logger) *Service {
	service := &Service{
		stackLocation: cfg.StackLocation,
		containers:    dockerClient,
		cgroupRoot:    "/sys/fs/cgroup",
		procRoot:      "/proc",
		logger:        logger.With(zap.String("component", "stats")),
		cache:         make(map[string]cacheEntry),
		previous:      make(map[string]stackSample),
		inflight:      make(map[string]*collection),
	}

	logger.Info("stats service initialized",
		zap.String("stack_location", cfg.StackLocation),
		zap.String("cgroup_root", service.cgroupRoot),
	)

	return service
}

func (s *Service) GetStackStats(name string) (*StackStats, error) {
	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		s.logger.Error("invalid stack name", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack '%s' not found", name)
	}

	return s.cachedStats(name)
}

func (s *Service) cachedStats(name string) (*StackStats, error) {
	s.mu.Lock()
	if entry, ok := s.cache[name]; ok && time.Since(entry.at) < cacheTTL {
		s.mu.Unlock()
		return entry.stats, nil
	}

	if inflight, ok := s.inflight[name]; ok {
		s.mu.Unlock()
		<-inflight.done
		return inflight.stats, inflight.err
	}

	inflight := &collection{done: make(chan struct{})}
	s.inflight[name] = inflight
	previous := s.previous[name]
	s.mu.Unlock()

	stats, current, err := s.collect(name, previous)

	s.mu.Lock()
	delete(s.inflight, name)
	if err == nil {
		s.cache[name] = cacheEntry{stats: stats, at: current.at}
		s.previous[name] = current
	}
	s.prune()
	s.mu.Unlock()

	inflight.stats, inflight.err = stats, err
	close(inflight.done)

	return stats, err
}

func (s *Service) prune() {
	cutoff := time.Now().Add(-sampleRetention)
	for name, entry := range s.cache {
		if entry.at.Before(cutoff) {
			delete(s.cache, name)
		}
	}
	for name, sample := range s.previous {
		if sample.at.Before(cutoff) {
			delete(s.previous, name)
		}
	}
}

func (s *Service) collect(name string, previous stackSample) (*StackStats, stackSample, error) {
	current, err := s.sampleStack(name)
	if err != nil {
		return nil, stackSample{}, err
	}

	return s.buildStats(name, current, previous, s.hostStats()), current, nil
}

func (s *Service) sampleStack(name string) (stackSample, error) {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()

	summaries, err := s.containers.ContainerList(ctx, map[string][]string{
		"label": {fmt.Sprintf("%s=%s", docker.LabelComposeProject, name)},
	})
	if err != nil {
		return stackSample{}, fmt.Errorf("failed to list containers for stack '%s': %w", name, err)
	}

	stack := stackSample{at: time.Now(), containers: make(map[string]containerSample, len(summaries))}

	for _, summary := range summaries {
		serviceName := summary.Labels[docker.LabelComposeService]
		if serviceName == "" {
			continue
		}

		sample := containerSample{
			name:        containerName(summary.Names),
			serviceName: serviceName,
			state:       summary.State,
		}

		if summary.State == "running" {
			s.sampleCgroup(&sample, summary.ID)
		}

		stack.containers[summary.ID] = sample
	}

	return stack, nil
}

func containerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func (s *Service) sampleCgroup(sample *containerSample, containerID string) {
	path := s.containerCgroupPath(containerID)
	if _, err := os.Stat(path); err != nil {
		s.logger.Debug("cgroup unavailable",
			zap.String("container", sample.name),
			zap.String("path", path),
			zap.Error(err),
		)
		return
	}

	memory := s.parseKeyedFile(filepath.Join(path, "memory.stat"))
	events := s.parseKeyedFile(filepath.Join(path, "memory.events"))
	cpu := s.parseKeyedFile(filepath.Join(path, "cpu.stat"))
	io := s.parseIOStat(filepath.Join(path, "io.stat"))

	sample.memoryCurrent = s.readUint(filepath.Join(path, "memory.current"))
	sample.memoryPeak = s.readUint(filepath.Join(path, "memory.peak"))
	sample.memoryLimit = s.readLimit(filepath.Join(path, "memory.max"))
	sample.memoryAnon = memory["anon"]
	sample.memoryFile = memory["file"]
	sample.memoryInactiveFile = memory["inactive_file"]
	sample.memorySwap = memory["swap"]
	sample.pageFaults = memory["pgfault"]
	sample.pageMajorFaults = memory["pgmajfault"]
	sample.oomKills = events["oom_kill"]
	sample.memoryLimitHits = events["max"]

	sample.cpuUsageUsec = cpu["usage_usec"]
	sample.cpuUserUsec = cpu["user_usec"]
	sample.cpuSystemUsec = cpu["system_usec"]
	sample.cpuThrottledUsec = cpu["throttled_usec"]
	sample.cpuPeriods = cpu["nr_periods"]
	sample.cpuThrottled = cpu["nr_throttled"]
	sample.cpuQuotaCores = s.readCPUQuota(filepath.Join(path, "cpu.max"))

	sample.blockReadBytes = io["rbytes"]
	sample.blockWriteBytes = io["wbytes"]
	sample.blockReadOps = io["rios"]
	sample.blockWriteOps = io["wios"]

	s.sampleNetwork(sample, path)

	sample.collected = true
}

func (s *Service) containerCgroupPath(containerID string) string {
	scope := filepath.Join(s.cgroupRoot, "system.slice", fmt.Sprintf("docker-%s.scope", containerID))
	if _, err := os.Stat(scope); err == nil {
		return scope
	}
	return filepath.Join(s.cgroupRoot, "docker", containerID)
}

func (s *Service) sampleNetwork(sample *containerSample, cgroupPath string) {
	pid, ok := s.firstProcess(filepath.Join(cgroupPath, "cgroup.procs"))
	if !ok {
		return
	}

	data, err := os.ReadFile(filepath.Join(s.procRoot, pid, "net", "dev"))
	if err != nil {
		s.logger.Debug("network stats unavailable",
			zap.String("container", sample.name),
			zap.String("pid", pid),
			zap.Error(err),
		)
		return
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		name, values, found := strings.Cut(line, ":")
		if !found {
			continue
		}

		if strings.TrimSpace(name) == "lo" {
			continue
		}

		fields := strings.Fields(values)
		if len(fields) < 10 {
			continue
		}

		sample.networkRxBytes += parseUint(fields[0])
		sample.networkRxPackets += parseUint(fields[1])
		sample.networkTxBytes += parseUint(fields[8])
		sample.networkTxPackets += parseUint(fields[9])
	}
}

func (s *Service) firstProcess(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	pid, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
	if pid == "" {
		return "", false
	}

	return pid, true
}

func (s *Service) buildStats(name string, current, previous stackSample, host HostStats) *StackStats {
	window := current.at.Sub(previous.at).Seconds()
	comparable := !previous.at.IsZero() && window > 0 && window <= maxSampleWindow.Seconds()

	stats := &StackStats{
		StackName:   name,
		CollectedAt: current.at,
		Host:        host,
		Containers:  make([]ContainerStats, 0, len(current.containers)),
	}

	if comparable {
		stats.SampleWindowSeconds = &window
	}

	for id, sample := range current.containers {
		entry := ContainerStats{
			Name:        sample.name,
			ServiceName: sample.serviceName,
			State:       sample.state,
		}

		if sample.collected {
			entry.CPUQuotaCores = sample.cpuQuotaCores
			entry.CPUUserTime = sample.cpuUserUsec / 1000
			entry.CPUSystemTime = sample.cpuSystemUsec / 1000

			entry.MemoryCurrent = sample.memoryCurrent
			entry.MemoryWorkingSet = saturatingSub(sample.memoryCurrent, sample.memoryInactiveFile)
			entry.MemoryAnon = sample.memoryAnon
			entry.MemoryFile = sample.memoryFile
			entry.MemoryInactiveFile = sample.memoryInactiveFile
			entry.MemorySwap = sample.memorySwap
			entry.MemoryPeak = sample.memoryPeak
			entry.MemoryLimit = sample.memoryLimit
			entry.OOMKills = sample.oomKills
			entry.MemoryLimitHits = sample.memoryLimitHits
			entry.PageFaults = sample.pageFaults
			entry.PageMajorFaults = sample.pageMajorFaults

			entry.NetworkRxBytes = sample.networkRxBytes
			entry.NetworkTxBytes = sample.networkTxBytes
			entry.NetworkRxPackets = sample.networkRxPackets
			entry.NetworkTxPackets = sample.networkTxPackets

			entry.BlockReadBytes = sample.blockReadBytes
			entry.BlockWriteBytes = sample.blockWriteBytes
			entry.BlockReadOps = sample.blockReadOps
			entry.BlockWriteOps = sample.blockWriteOps

			if sample.memoryLimit > 0 {
				entry.MemoryPercentOfLimit = ratio(entry.MemoryWorkingSet, sample.memoryLimit)
			}
			if host.MemoryTotal > 0 {
				entry.MemoryPercentOfHost = ratio(entry.MemoryWorkingSet, host.MemoryTotal)
			}
		}

		prior, seen := previous.containers[id]
		if comparable && seen && sample.collected && prior.collected {
			s.applyRates(&entry, sample, prior, window, host)
		}

		stats.Containers = append(stats.Containers, entry)
	}

	sort.Slice(stats.Containers, func(i, j int) bool {
		return stats.Containers[i].Name < stats.Containers[j].Name
	})

	return stats
}

func (s *Service) applyRates(entry *ContainerStats, sample, prior containerSample, window float64, host HostStats) {
	entry.NetworkRxBytesPerSecond = rate(sample.networkRxBytes, prior.networkRxBytes, window)
	entry.NetworkTxBytesPerSecond = rate(sample.networkTxBytes, prior.networkTxBytes, window)
	entry.BlockReadBytesPerSecond = rate(sample.blockReadBytes, prior.blockReadBytes, window)
	entry.BlockWriteBytesPerSecond = rate(sample.blockWriteBytes, prior.blockWriteBytes, window)

	usage := rate(sample.cpuUsageUsec, prior.cpuUsageUsec, window)
	if usage == nil {
		return
	}

	cores := *usage / 1_000_000
	entry.CPUUsageCores = &cores

	if sample.cpuQuotaCores > 0 {
		percent := cores / sample.cpuQuotaCores * 100
		entry.CPUPercentOfQuota = &percent
	}

	if host.CPUCores > 0 {
		percent := cores / float64(host.CPUCores) * 100
		entry.CPUPercentOfHost = &percent
	}

	if periods := saturatingSub(sample.cpuPeriods, prior.cpuPeriods); periods > 0 {
		throttled := saturatingSub(sample.cpuThrottled, prior.cpuThrottled)
		entry.CPUThrottledPercent = ratio(throttled, periods)
	}
}

func rate(current, prior uint64, window float64) *float64 {
	if current < prior || window <= 0 {
		return nil
	}

	value := float64(current-prior) / window
	return &value
}

func ratio(part, whole uint64) *float64 {
	if whole == 0 {
		return nil
	}

	value := float64(part) / float64(whole) * 100
	return &value
}

func saturatingSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

func (s *Service) hostStats() HostStats {
	memory := s.parseMeminfo()
	load1, load5, load15 := s.parseLoadAverage()

	return HostStats{
		MemoryTotal:     memory["MemTotal"] * 1024,
		MemoryAvailable: memory["MemAvailable"] * 1024,
		CPUCores:        s.hostCPUCores(),
		Load1:           load1,
		Load5:           load5,
		Load15:          load15,
	}
}

func (s *Service) parseMeminfo() map[string]uint64 {
	values := make(map[string]uint64, 2)

	data, err := os.ReadFile(filepath.Join(s.procRoot, "meminfo"))
	if err != nil {
		s.logger.Debug("host memory unavailable", zap.Error(err))
		return values
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}

		if key != "MemTotal" && key != "MemAvailable" {
			continue
		}

		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}

		values[key] = parseUint(fields[0])
	}

	return values
}

func (s *Service) parseLoadAverage() (float64, float64, float64) {
	data, err := os.ReadFile(filepath.Join(s.procRoot, "loadavg"))
	if err != nil {
		s.logger.Debug("host load average unavailable", zap.Error(err))
		return 0, 0, 0
	}

	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}

	load1, _ := strconv.ParseFloat(fields[0], 64)
	load5, _ := strconv.ParseFloat(fields[1], 64)
	load15, _ := strconv.ParseFloat(fields[2], 64)

	return load1, load5, load15
}

func (s *Service) hostCPUCores() int {
	data, err := os.ReadFile(filepath.Join(s.procRoot, "stat"))
	if err != nil {
		s.logger.Debug("host cpu count unavailable", zap.Error(err))
		return 0
	}

	cores := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		field, _, _ := strings.Cut(line, " ")
		if len(field) > 3 && strings.HasPrefix(field, "cpu") && field[3] >= '0' && field[3] <= '9' {
			cores++
		}
	}

	return cores
}

func (s *Service) parseKeyedFile(path string) map[string]uint64 {
	values := make(map[string]uint64)

	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		values[fields[0]] = value
	}

	return values
}

func (s *Service) parseIOStat(path string) map[string]uint64 {
	values := make(map[string]uint64)

	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		for _, field := range fields[1:] {
			key, value, found := strings.Cut(field, "=")
			if !found {
				continue
			}

			parsed, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				continue
			}

			values[key] += parsed
		}
	}

	return values
}

func (s *Service) readUint(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	return parseUint(strings.TrimSpace(string(data)))
}

func (s *Service) readLimit(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	content := strings.TrimSpace(string(data))
	if content == "max" {
		return 0
	}

	return parseUint(content)
}

func (s *Service) readCPUQuota(path string) float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	quota, period, found := strings.Cut(strings.TrimSpace(string(data)), " ")
	if !found || quota == "max" {
		return 0
	}

	quotaUsec := parseUint(quota)
	periodUsec := parseUint(period)
	if quotaUsec == 0 || periodUsec == 0 {
		return 0
	}

	return float64(quotaUsec) / float64(periodUsec)
}

func parseUint(value string) uint64 {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}

	return parsed
}
