package compose

type ComposeChanges struct {
	ServiceImageUpdates []ServiceImageUpdate `json:"service_image_updates,omitempty"`
}

type ServiceImageUpdate struct {
	ServiceName string `json:"service_name" binding:"required"`
	NewImage    string `json:"new_image,omitempty"`
	NewTag      string `json:"new_tag,omitempty"`
}

type UpdateComposeRequest struct {
	StackName string         `json:"stack_name" binding:"required"`
	Changes   ComposeChanges `json:"changes" binding:"required"`
}
