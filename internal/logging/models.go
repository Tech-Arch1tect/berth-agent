package logging

import "time"

type RequestLogEntry struct {
	Timestamp     string            `json:"timestamp"`
	RequestID     string            `json:"request_id"`
	SourceIP      string            `json:"source_ip"`
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	UserAgent     string            `json:"user_agent,omitempty"`
	AuthStatus    string            `json:"auth_status"`
	AuthError     string            `json:"auth_error,omitempty"`
	AuthTokenHash string            `json:"auth_token_hash,omitempty"`
	StatusCode    int               `json:"status_code"`
	ResponseSize  int64             `json:"response_size"`
	LatencyMs     float64           `json:"latency_ms"`
	Error         string            `json:"error,omitempty"`
	StackName     string            `json:"stack_name,omitempty"`
	ContainerName string            `json:"container_name,omitempty"`
	Operation     string            `json:"operation,omitempty"`
	OperationID   string            `json:"operation_id,omitempty"`
	FilePath      string            `json:"file_path,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

func NewRequestLogEntry() *RequestLogEntry {
	return &RequestLogEntry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		AuthStatus: "none",
		Metadata:   make(map[string]string),
	}
}
