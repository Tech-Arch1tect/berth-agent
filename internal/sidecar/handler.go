package sidecar

import (
	"fmt"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type Handler struct {
	service *Service
	logger  *logging.Logger
}

func NewHandler(service *Service, logger *logging.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger,
	}
}

func (h *Handler) HandleOperation(c echo.Context) error {
	h.logger.Info("sidecar received operation request",
		zap.String("remote_ip", c.RealIP()),
	)

	var req OperationRequest
	if err := c.Bind(&req); err != nil {
		h.logger.Error("sidecar failed to bind request",
			zap.String("remote_ip", c.RealIP()),
			zap.Error(err),
		)
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid request format",
		})
	}

	h.logger.Info("sidecar executing operation",
		zap.String("command", req.Command),
		zap.String("stack_path", req.StackPath),
		zap.Strings("options", req.Options),
		zap.Strings("services", req.Services),
	)

	if err := h.service.ExecuteOperation(c.Request().Context(), req); err != nil {
		h.logger.Error("sidecar operation failed",
			zap.String("command", req.Command),
			zap.String("stack_path", req.StackPath),
			zap.Error(err),
		)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Sidecar operation failed: %v", err),
		})
	}

	h.logger.Info("sidecar operation completed successfully",
		zap.String("command", req.Command),
		zap.String("stack_path", req.StackPath),
	)
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
