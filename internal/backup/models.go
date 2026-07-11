package backup

import "time"

type ComponentKind string

const (
	KindStackDirectory  ComponentKind = "stack-directory"
	KindVolume          ComponentKind = "volume"
	KindBindMount       ComponentKind = "bind-mount"
	KindAnonymousVolume ComponentKind = "anonymous-volume"
)

type Component struct {
	ID              string        `json:"id"`
	Kind            ComponentKind `json:"kind"`
	VolumeName      string        `json:"volume_name,omitempty"`
	SourcePath      string        `json:"source_path,omitempty"`
	Service         string        `json:"service,omitempty"`
	Target          string        `json:"target,omitempty"`
	Excludes        []string      `json:"excludes,omitempty"`
	SnapshotID      string        `json:"snapshot_id,omitempty"`
	FilesNew        uint64        `json:"files_new"`
	FilesChanged    uint64        `json:"files_changed"`
	FilesUnmodified uint64        `json:"files_unmodified"`
	BytesAdded      uint64        `json:"bytes_added"`
	BytesProcessed  uint64        `json:"bytes_processed"`
	DurationSecs    float64       `json:"duration_secs"`
	Error           string        `json:"error,omitempty"`
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
	Components    []Component    `json:"components"`
	Skipped       []SkippedMount `json:"skipped,omitempty"`
	Error         string         `json:"error,omitempty"`
}

type CreateOptions struct {
	StopMode string
}

type ProgressWriter interface {
	WriteStdout(message string)
	WriteStderr(message string)
	WriteProgress(message string)
}
