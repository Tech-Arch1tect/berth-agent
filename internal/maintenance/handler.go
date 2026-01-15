package maintenance

import (
	"berth-agent/internal/audit"
	"net/http"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	maintenanceService *Service
	auditService       *audit.Service
}

func NewHandler(maintenanceService *Service, auditService *audit.Service) *Handler {
	return &Handler{
		maintenanceService: maintenanceService,
		auditService:       auditService,
	}
}

func (h *Handler) GetSystemInfo(c echo.Context) error {
	ctx := c.Request().Context()

	info, err := h.maintenanceService.GetSystemInfo(ctx)
	if err != nil {
		h.auditService.LogMaintenanceEvent(audit.EventMaintenanceGetInfo, c.RealIP(), false, err.Error(), nil)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	h.auditService.LogMaintenanceEvent(audit.EventMaintenanceGetInfo, c.RealIP(), true, "", nil)

	return c.JSON(http.StatusOK, info)
}

func (h *Handler) PruneDocker(c echo.Context) error {
	ctx := c.Request().Context()

	var req PruneRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	if req.Type == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Prune type is required"})
	}

	result, err := h.maintenanceService.PruneDocker(ctx, &req)
	if err != nil {
		h.auditService.LogMaintenanceEvent(audit.EventMaintenancePrune, c.RealIP(), false, err.Error(), map[string]any{
			"type":  req.Type,
			"force": req.Force,
			"all":   req.All,
		})
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	h.auditService.LogMaintenanceEvent(audit.EventMaintenancePrune, c.RealIP(), true, "", map[string]any{
		"type":  req.Type,
		"force": req.Force,
		"all":   req.All,
	})

	return c.JSON(http.StatusOK, result)
}

func (h *Handler) DeleteResource(c echo.Context) error {
	ctx := c.Request().Context()

	var req DeleteRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	if req.Type == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Resource type is required"})
	}

	if req.ID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Resource ID is required"})
	}

	result, err := h.maintenanceService.DeleteResource(ctx, &req)
	if err != nil {
		h.auditService.LogMaintenanceEvent(audit.EventMaintenanceDeleteResource, c.RealIP(), false, err.Error(), map[string]any{
			"resource_type": req.Type,
			"resource_id":   req.ID,
		})
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	h.auditService.LogMaintenanceEvent(audit.EventMaintenanceDeleteResource, c.RealIP(), true, "", map[string]any{
		"resource_type": req.Type,
		"resource_id":   req.ID,
	})

	return c.JSON(http.StatusOK, result)
}
