package stack

import (
	"github.com/tech-arch1tect/berth-agent/internal/audit"
	"github.com/tech-arch1tect/berth-agent/internal/common"
	"github.com/tech-arch1tect/berth-agent/internal/validation"
	"strings"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	service      *Service
	auditService *audit.Service
}

func NewHandler(service *Service, auditService *audit.Service) *Handler {
	return &Handler{
		service:      service,
		auditService: auditService,
	}
}

func (h *Handler) ListStacks(c echo.Context) error {
	stacks, err := h.service.ListStacks()
	if err != nil {
		h.auditService.LogStackEvent(audit.EventStackList, c.RealIP(), "", false, err.Error(), nil)
		return common.SendInternalError(c, err.Error())
	}

	h.auditService.LogStackEvent(audit.EventStackList, c.RealIP(), "", true, "", map[string]any{
		"count": len(stacks),
	})

	return common.SendSuccess(c, stacks)
}

func (h *Handler) CreateStack(c echo.Context) error {
	var req CreateStackRequest
	if err := c.Bind(&req); err != nil {
		return common.SendBadRequest(c, "invalid request body")
	}

	if req.Name == "" {
		return common.SendBadRequest(c, "stack name is required")
	}

	stack, err := h.service.CreateStack(req.Name)
	if err != nil {
		h.auditService.LogStackEvent(audit.EventStackCreate, c.RealIP(), req.Name, false, err.Error(), nil)
		if strings.Contains(err.Error(), "already exists") {
			return common.SendConflict(c, err.Error())
		}
		return common.SendBadRequest(c, err.Error())
	}

	h.auditService.LogStackEvent(audit.EventStackCreate, c.RealIP(), req.Name, true, "", nil)

	return common.SendCreated(c, CreateStackResponse{
		Success: true,
		Message: "Stack created successfully",
		Stack:   stack,
	})
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
		h.auditService.LogStackEvent(audit.EventStackGetDetails, c.RealIP(), stackName, false, err.Error(), nil)
		return common.SendNotFound(c, err.Error())
	}

	h.auditService.LogStackEvent(audit.EventStackGetDetails, c.RealIP(), stackName, true, "", nil)

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
		h.auditService.LogStackEvent(audit.EventStackGetNetworks, c.RealIP(), stackName, false, err.Error(), nil)
		return common.SendNotFound(c, err.Error())
	}

	h.auditService.LogStackEvent(audit.EventStackGetNetworks, c.RealIP(), stackName, true, "", nil)

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
		h.auditService.LogStackEvent(audit.EventStackGetVolumes, c.RealIP(), stackName, false, err.Error(), nil)
		return common.SendNotFound(c, err.Error())
	}

	h.auditService.LogStackEvent(audit.EventStackGetVolumes, c.RealIP(), stackName, true, "", nil)

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

	unmask := c.QueryParam("unmask") == "true"

	envVars, err := h.service.GetStackEnvironmentVariables(stackName, unmask)
	if err != nil {
		h.auditService.LogStackEvent(audit.EventStackGetEnvVars, c.RealIP(), stackName, false, err.Error(), map[string]any{
			"unmask": unmask,
		})
		return common.SendNotFound(c, err.Error())
	}

	h.auditService.LogStackEvent(audit.EventStackGetEnvVars, c.RealIP(), stackName, true, "", map[string]any{
		"unmask": unmask,
	})

	return common.SendSuccess(c, envVars)
}

func (h *Handler) GetStacksSummary(c echo.Context) error {
	patternsParam := c.QueryParam("patterns")
	var patterns []string

	if patternsParam != "" {
		patterns = strings.Split(patternsParam, ",")
		for i, pattern := range patterns {
			patterns[i] = strings.TrimSpace(pattern)
		}
	}

	if len(patterns) == 0 {
		patterns = []string{"*"}
	}

	summary, err := h.service.GetStacksSummary(patterns)
	if err != nil {
		h.auditService.LogStackEvent(audit.EventStackGetSummary, c.RealIP(), "", false, err.Error(), map[string]any{
			"patterns": patterns,
		})
		return common.SendInternalError(c, err.Error())
	}

	h.auditService.LogStackEvent(audit.EventStackGetSummary, c.RealIP(), "", true, "", map[string]any{
		"patterns": patterns,
	})

	return common.SendSuccess(c, summary)
}

func (h *Handler) GetContainerImageDetails(c echo.Context) error {
	stackName := c.Param("name")
	if stackName == "" {
		return common.SendBadRequest(c, "stack name is required")
	}

	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "invalid stack name: "+err.Error())
	}

	imageDetails, err := h.service.GetContainerImageDetails(stackName)
	if err != nil {
		h.auditService.LogStackEvent(audit.EventStackGetImages, c.RealIP(), stackName, false, err.Error(), nil)
		return common.SendNotFound(c, err.Error())
	}

	h.auditService.LogStackEvent(audit.EventStackGetImages, c.RealIP(), stackName, true, "", nil)

	return common.SendSuccess(c, imageDetails)
}
