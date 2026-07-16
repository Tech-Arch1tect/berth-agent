package backup

import "time"

type ComponentKind string

const (
	KindStackDirectory  ComponentKind = "stack-directory"
	KindVolume          ComponentKind = "volume"
	KindBindMount       ComponentKind = "bind-mount"
	KindAnonymousVolume ComponentKind = "anonymous-volume"
)

type VolumeDefinition struct {
	External   bool              `json:"external,omitempty"`
	Driver     string            `json:"driver,omitempty"`
	DriverOpts map[string]string `json:"driver_opts,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

type Component struct {
	ID              string            `json:"id"`
	Kind            ComponentKind     `json:"kind"`
	VolumeName      string            `json:"volume_name,omitempty"`
	VolumeDef       *VolumeDefinition `json:"volume_def,omitempty"`
	SourcePath      string            `json:"source_path,omitempty"`
	Service         string            `json:"service,omitempty"`
	Target          string            `json:"target,omitempty"`
	ContainerNumber string            `json:"container_number,omitempty"`
	Excludes        []string          `json:"excludes,omitempty"`
	SnapshotID      string            `json:"snapshot_id,omitempty"`
	FilesNew        uint64            `json:"files_new"`
	FilesChanged    uint64            `json:"files_changed"`
	FilesUnmodified uint64            `json:"files_unmodified"`
	BytesAdded      uint64            `json:"bytes_added"`
	BytesProcessed  uint64            `json:"bytes_processed"`
	DurationSecs    float64           `json:"duration_secs"`
	Error           string            `json:"error,omitempty"`
}

type SkippedMount struct {
	Kind    string `json:"kind"`
	Service string `json:"service,omitempty"`
	Target  string `json:"target,omitempty"`
	Reason  string `json:"reason"`
}

type RunStatus string

const (
	StatusRunning     RunStatus = "running"
	StatusCompleted   RunStatus = "completed"
	StatusFailed      RunStatus = "failed"
	StatusInterrupted RunStatus = "interrupted"
)

type Run struct {
	ID            string         `json:"id"`
	StackName     string         `json:"stack_name"`
	StartedAt     time.Time      `json:"started_at"`
	FinishedAt    *time.Time     `json:"finished_at,omitempty"`
	Status        RunStatus      `json:"status"`
	StopMode      string         `json:"stop_mode,omitempty"`
	ResticVersion string         `json:"restic_version,omitempty"`
	Verified      *bool          `json:"verified,omitempty"`
	VerifyError   string         `json:"verify_error,omitempty"`
	RepoSizeBytes uint64         `json:"repo_size_bytes,omitempty"`
	Components    []Component    `json:"components"`
	Skipped       []SkippedMount `json:"skipped,omitempty"`
	Error         string         `json:"error,omitempty"`
}

type RunSummary struct {
	ID                   string     `json:"id"`
	StackName            string     `json:"stack_name"`
	StartedAt            time.Time  `json:"started_at"`
	FinishedAt           *time.Time `json:"finished_at,omitempty"`
	Status               RunStatus  `json:"status"`
	StopMode             string     `json:"stop_mode,omitempty"`
	Verified             *bool      `json:"verified,omitempty"`
	RepoSizeBytes        uint64     `json:"repo_size_bytes,omitempty"`
	SizeBytes            uint64     `json:"size_bytes"`
	AddedBytes           uint64     `json:"added_bytes"`
	ComponentCount       int        `json:"component_count"`
	ComponentsWithErrors int        `json:"components_with_errors"`
}

func SummariseRun(run *Run) RunSummary {
	summary := RunSummary{
		ID:             run.ID,
		StackName:      run.StackName,
		StartedAt:      run.StartedAt,
		FinishedAt:     run.FinishedAt,
		Status:         run.Status,
		StopMode:       run.StopMode,
		Verified:       run.Verified,
		RepoSizeBytes:  run.RepoSizeBytes,
		ComponentCount: len(run.Components),
	}
	for _, component := range run.Components {
		summary.SizeBytes += component.BytesProcessed
		summary.AddedBytes += component.BytesAdded
		if component.Error != "" {
			summary.ComponentsWithErrors++
		}
	}
	return summary
}

type CreateOptions struct {
	StopMode string
	Password string
}

type ProgressWriter interface {
	WriteStdout(message string)
	WriteStderr(message string)
	WriteProgress(message string)
}
