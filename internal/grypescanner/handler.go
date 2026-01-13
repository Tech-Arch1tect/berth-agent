package grypescanner

import (
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

func (h *Handler) Scan(c echo.Context) error {
	var req ScanRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
	}

	if req.Image == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "image is required",
		})
	}

	response, err := h.service.ScanImage(c.Request().Context(), req.Image)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, response)
}

func (h *Handler) Health(c echo.Context) error {
	available := h.service.IsAvailable()
	status := "ok"
	if !available {
		status = "grype unavailable"
	}

	return c.JSON(http.StatusOK, HealthResponse{
		Status:    status,
		Available: available,
	})
}
