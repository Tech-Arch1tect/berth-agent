package composeeditor

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

func (h *Handler) GetComposeConfig(c echo.Context) error {
	stackName := c.Param("name")
	if stackName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "stack name is required",
		})
	}

	config, err := h.service.GetComposeConfig(c.Request().Context(), stackName)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, config)
}
