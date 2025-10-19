package images

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CheckImageUpdates(c echo.Context) error {
	var req CheckImageUpdatesRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	results, err := h.service.CheckImageUpdates(c.Request().Context(), req.RegistryCredentials, req.DisabledRegistries)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}

	return c.JSON(http.StatusOK, CheckImageUpdatesResponse{Results: results})
}
