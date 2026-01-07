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

func (h *Handler) UpdateCompose(c echo.Context) error {
	stackName := c.Param("name")
	if stackName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "stack name is required",
		})
	}

	var req UpdateComposeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
	}

	if req.Preview {
		originalYaml, modifiedYaml, err := h.service.PreviewCompose(c.Request().Context(), stackName, req.Changes)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
		}
		return c.JSON(http.StatusOK, UpdateComposeResponse{
			Success:      true,
			OriginalYaml: originalYaml,
			ModifiedYaml: modifiedYaml,
		})
	}

	if err := h.service.UpdateCompose(c.Request().Context(), stackName, req.Changes); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, UpdateComposeResponse{
		Success: true,
		Message: "Compose file updated successfully",
	})
}
