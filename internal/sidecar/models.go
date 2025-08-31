package sidecar

type OperationRequest struct {
	Command   string   `json:"command"`
	Options   []string `json:"options"`
	Services  []string `json:"services"`
	StackPath string   `json:"stack_path"`
}
