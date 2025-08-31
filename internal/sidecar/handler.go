package sidecar

import (
	"fmt"
	"net/http"

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

func (h *Handler) HandleOperation(c echo.Context) error {
	var req OperationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid request format",
		})
	}

	if err := h.service.ExecuteOperation(c.Request().Context(), req); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Sidecar operation failed: %v", err),
		})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "Sidecar operation completed successfully",
		"command": req.Command,
	})
}

func (h *Handler) Health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status":  "healthy",
		"service": "berth-agent-sidecar",
	})
}
