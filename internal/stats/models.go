package stats

import "time"

type ContainerStats struct {
	Name        string `json:"name"`
	ServiceName string `json:"service_name"`
	State       string `json:"state"`

	CPUUsageCores       *float64 `json:"cpu_usage_cores"`
	CPUQuotaCores       float64  `json:"cpu_quota_cores"`
	CPUPercentOfQuota   *float64 `json:"cpu_percent_of_quota"`
	CPUPercentOfHost    *float64 `json:"cpu_percent_of_host"`
	CPUThrottledPercent *float64 `json:"cpu_throttled_percent"`
	CPUUserTime         uint64   `json:"cpu_user_time"`
	CPUSystemTime       uint64   `json:"cpu_system_time"`

	MemoryWorkingSet     uint64   `json:"memory_working_set"`
	MemoryCurrent        uint64   `json:"memory_current"`
	MemoryAnon           uint64   `json:"memory_anon"`
	MemoryFile           uint64   `json:"memory_file"`
	MemoryInactiveFile   uint64   `json:"memory_inactive_file"`
	MemorySwap           uint64   `json:"memory_swap"`
	MemoryPeak           uint64   `json:"memory_peak"`
	MemoryLimit          uint64   `json:"memory_limit"`
	MemoryPercentOfLimit *float64 `json:"memory_percent_of_limit"`
	MemoryPercentOfHost  *float64 `json:"memory_percent_of_host"`
	OOMKills             uint64   `json:"oom_kills"`
	MemoryLimitHits      uint64   `json:"memory_limit_hits"`
	PageFaults           uint64   `json:"page_faults"`
	PageMajorFaults      uint64   `json:"page_major_faults"`

	NetworkRxBytes          uint64   `json:"network_rx_bytes"`
	NetworkTxBytes          uint64   `json:"network_tx_bytes"`
	NetworkRxPackets        uint64   `json:"network_rx_packets"`
	NetworkTxPackets        uint64   `json:"network_tx_packets"`
	NetworkRxBytesPerSecond *float64 `json:"network_rx_bytes_per_second"`
	NetworkTxBytesPerSecond *float64 `json:"network_tx_bytes_per_second"`

	BlockReadBytes           uint64   `json:"block_read_bytes"`
	BlockWriteBytes          uint64   `json:"block_write_bytes"`
	BlockReadOps             uint64   `json:"block_read_ops"`
	BlockWriteOps            uint64   `json:"block_write_ops"`
	BlockReadBytesPerSecond  *float64 `json:"block_read_bytes_per_second"`
	BlockWriteBytesPerSecond *float64 `json:"block_write_bytes_per_second"`
}

type HostStats struct {
	MemoryTotal     uint64  `json:"memory_total"`
	MemoryAvailable uint64  `json:"memory_available"`
	CPUCores        int     `json:"cpu_cores"`
	Load1           float64 `json:"load_1"`
	Load5           float64 `json:"load_5"`
	Load15          float64 `json:"load_15"`
}

type StackStats struct {
	StackName           string           `json:"stack_name"`
	CollectedAt         time.Time        `json:"collected_at"`
	SampleWindowSeconds *float64         `json:"sample_window_seconds"`
	Host                HostStats        `json:"host"`
	Containers          []ContainerStats `json:"containers"`
}
