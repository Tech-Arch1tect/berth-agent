package stats

type ContainerStats struct {
	Name             string  `json:"name"`
	ServiceName      string  `json:"service_name"`
	CPUPercent       float64 `json:"cpu_percent"`
	CPUUserTime      uint64  `json:"cpu_user_time"`
	CPUSystemTime    uint64  `json:"cpu_system_time"`
	MemoryUsage      uint64  `json:"memory_usage"`
	MemoryLimit      uint64  `json:"memory_limit"`
	MemoryPercent    float64 `json:"memory_percent"`
	MemoryRSS        uint64  `json:"memory_rss"`
	MemoryCache      uint64  `json:"memory_cache"`
	MemorySwap       uint64  `json:"memory_swap"`
	PageFaults       uint64  `json:"page_faults"`
	PageMajorFaults  uint64  `json:"page_major_faults"`
	NetworkRxBytes   uint64  `json:"network_rx_bytes"`
	NetworkTxBytes   uint64  `json:"network_tx_bytes"`
	NetworkRxPackets uint64  `json:"network_rx_packets"`
	NetworkTxPackets uint64  `json:"network_tx_packets"`
	BlockReadBytes   uint64  `json:"block_read_bytes"`
	BlockWriteBytes  uint64  `json:"block_write_bytes"`
	BlockReadOps     uint64  `json:"block_read_ops"`
	BlockWriteOps    uint64  `json:"block_write_ops"`
}

type StackStats struct {
	StackName  string           `json:"stack_name"`
	Containers []ContainerStats `json:"containers"`
}
