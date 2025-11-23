package compose

import (
	"berth-agent/internal/common"
	"berth-agent/internal/validation"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) PreviewChanges(c echo.Context) error {
	var req UpdateComposeRequest
	if err := c.Bind(&req); err != nil {
		return common.SendBadRequest(c, "invalid request body")
	}

	h.service.logger.Debug("preview request received",
		zap.String("stack_name", req.StackName),
		zap.Int("image_updates", len(req.Changes.ServiceImageUpdates)),
		zap.Int("port_updates", len(req.Changes.ServicePortUpdates)),
	)

	if err := validation.ValidateStackName(req.StackName); err != nil {
		return common.SendBadRequest(c, "invalid stack name: "+err.Error())
	}

	original, preview, err := h.service.PreviewChanges(req.StackName, req.Changes)
	if err != nil {
		h.service.logger.Error("preview generation failed",
			zap.String("stack_name", req.StackName),
			zap.Error(err),
		)
		return common.SendInternalError(c, err.Error())
	}

	h.service.logger.Debug("preview generated",
		zap.String("stack_name", req.StackName),
		zap.Int("original_length", len(original)),
		zap.Int("preview_length", len(preview)),
	)

	response := PreviewComposeResponse{
		Original: original,
		Preview:  preview,
	}

	return common.SendSuccess(c, map[string]any{
		"data": response,
	})
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
