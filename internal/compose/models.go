package compose

type ComposeChanges struct {
	ServiceImageUpdates []ServiceImageUpdate `json:"service_image_updates,omitempty"`
	ServicePortUpdates  []ServicePortUpdate  `json:"service_port_updates,omitempty"`
}

type ServiceImageUpdate struct {
	ServiceName string `json:"service_name" binding:"required"`
	NewImage    string `json:"new_image,omitempty"`
	NewTag      string `json:"new_tag,omitempty"`
}

type ServicePortUpdate struct {
	ServiceName string   `json:"service_name" binding:"required"`
	Ports       []string `json:"ports"`
}

type UpdateComposeRequest struct {
	StackName string         `json:"stack_name" binding:"required"`
	Changes   ComposeChanges `json:"changes" binding:"required"`
}

type PreviewComposeResponse struct {
	Original string `json:"original"`
	Preview  string `json:"preview"`
}
