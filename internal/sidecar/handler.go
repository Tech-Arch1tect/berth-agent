package sidecar

import (
	"fmt"
	"log"
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
	log.Printf("Sidecar received operation request from %s", c.RealIP())

	var req OperationRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("Failed to bind sidecar request: %v", err)
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid request format",
		})
	}

	log.Printf("Sidecar executing operation - Command: %s, StackPath: %s, Options: %v",
		req.Command, req.StackPath, req.Options)

	if err := h.service.ExecuteOperation(c.Request().Context(), req); err != nil {
		log.Printf("Sidecar operation failed - Command: %s, Error: %v", req.Command, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Sidecar operation failed: %v", err),
		})
	}

	log.Printf("Sidecar operation completed successfully - Command: %s", req.Command)
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
