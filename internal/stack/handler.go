package stack

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

func (h *Handler) ListStacks(c echo.Context) error {
	stacks, err := h.service.ListStacks()
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}
	return common.SendSuccess(c, stacks)
}

func (h *Handler) GetStackDetails(c echo.Context) error {
	stackName := c.Param("name")
	if stackName == "" {
		return common.SendBadRequest(c, "stack name is required")
	}

	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "invalid stack name: "+err.Error())
	}

	stackDetails, err := h.service.GetStackDetails(stackName)
	if err != nil {
		return common.SendNotFound(c, err.Error())
	}

	return common.SendSuccess(c, stackDetails)
}

func (h *Handler) GetStackNetworks(c echo.Context) error {
	stackName := c.Param("name")
	if stackName == "" {
		return common.SendBadRequest(c, "stack name is required")
	}

	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "invalid stack name: "+err.Error())
	}

	networks, err := h.service.GetStackNetworks(stackName)
	if err != nil {
		return common.SendNotFound(c, err.Error())
	}

	return common.SendSuccess(c, networks)
}

func (h *Handler) GetStackVolumes(c echo.Context) error {
	stackName := c.Param("name")
	if stackName == "" {
		return common.SendBadRequest(c, "stack name is required")
	}

	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "invalid stack name: "+err.Error())
	}

	volumes, err := h.service.GetStackVolumes(stackName)
	if err != nil {
		return common.SendNotFound(c, err.Error())
	}

	return common.SendSuccess(c, volumes)
}

func (h *Handler) GetStackEnvironmentVariables(c echo.Context) error {
	stackName := c.Param("name")
	if stackName == "" {
		return common.SendBadRequest(c, "stack name is required")
	}

	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "invalid stack name: "+err.Error())
	}

	envVars, err := h.service.GetStackEnvironmentVariables(stackName)
	if err != nil {
		return common.SendNotFound(c, err.Error())
	}

	return common.SendSuccess(c, envVars)
}
