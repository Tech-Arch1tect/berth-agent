package stats

type ContainerStats struct {
	Name          string  `json:"name"`
	ServiceName   string  `json:"service_name"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryUsage   uint64  `json:"memory_usage"`
	MemoryLimit   uint64  `json:"memory_limit"`
	MemoryPercent float64 `json:"memory_percent"`
}

type StackStats struct {
	StackName  string           `json:"stack_name"`
	Containers []ContainerStats `json:"containers"`
}
