package sidecar

type OperationRequest struct {
	Command string   `json:"command"`
	Options []string `json:"options"`
}
