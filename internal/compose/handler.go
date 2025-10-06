package compose

import (
	"berth-agent/internal/common"
	"berth-agent/internal/validation"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) UpdateCompose(c echo.Context) error {
	var req UpdateComposeRequest
	if err := c.Bind(&req); err != nil {
		return common.SendBadRequest(c, "invalid request body")
	}

	if err := validation.ValidateStackName(req.StackName); err != nil {
		return common.SendBadRequest(c, "invalid stack name: "+err.Error())
	}

	if err := h.service.UpdateCompose(req.StackName, req.Changes); err != nil {
		return common.SendInternalError(c, err.Error())
	}

	return common.SendSuccess(c, map[string]string{
		"message": "compose file updated successfully",
	})
}
