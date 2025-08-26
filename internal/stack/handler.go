package stack

import (
	"berth-agent/internal/validation"
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

func (h *Handler) ListStacks(c echo.Context) error {
	stacks, err := h.service.ListStacks()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}
	return c.JSON(http.StatusOK, stacks)
}

func (h *Handler) GetStackDetails(c echo.Context) error {
	stackName := c.Param("name")
	if stackName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "stack name is required",
		})
	}

	if err := validation.ValidateStackName(stackName); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid stack name: " + err.Error(),
		})
	}

	stackDetails, err := h.service.GetStackDetails(stackName)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, stackDetails)
}

func (h *Handler) GetStackNetworks(c echo.Context) error {
	stackName := c.Param("name")
	if stackName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "stack name is required",
		})
	}

	if err := validation.ValidateStackName(stackName); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid stack name: " + err.Error(),
		})
	}

	networks, err := h.service.GetStackNetworks(stackName)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, networks)
}
