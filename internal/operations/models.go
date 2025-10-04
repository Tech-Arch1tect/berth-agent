package operations

import "time"

type RegistryCredential struct {
	Registry string `json:"registry"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type OperationRequest struct {
	Command             string               `json:"command"`
	Options             []string             `json:"options"`
	Services            []string             `json:"services"`
	RegistryCredentials []RegistryCredential `json:"registry_credentials,omitempty"`
}

type OperationResponse struct {
	OperationID string `json:"operationId"`
}

type StreamMessage struct {
	Type      string    `json:"type"`
	Data      string    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

type StreamMessageType string

const (
	StreamTypeStdout   StreamMessageType = "stdout"
	StreamTypeStderr   StreamMessageType = "stderr"
	StreamTypeProgress StreamMessageType = "progress"
	StreamTypeComplete StreamMessageType = "complete"
	StreamTypeError    StreamMessageType = "error"
)

type CompleteMessage struct {
	Type     StreamMessageType `json:"type"`
	Success  bool              `json:"success"`
	ExitCode int               `json:"exitCode"`
}

type Operation struct {
	ID        string
	StackName string
	Request   OperationRequest
	StartTime time.Time
	Status    string
	ExitCode  *int
	IsSelfOp  bool
}
