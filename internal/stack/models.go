package stack

type CreateStackRequest struct {
	Name string `json:"name" validate:"required"`
}

type CreateStackResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Stack   *Stack `json:"stack,omitempty"`
}
