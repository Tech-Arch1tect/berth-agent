package maintenance

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	maintenanceService *Service
}

func NewHandler(maintenanceService *Service) *Handler {
	return &Handler{
		maintenanceService: maintenanceService,
	}
}

func (h *Handler) GetSystemInfo(c echo.Context) error {
	ctx := c.Request().Context()

	info, err := h.maintenanceService.GetSystemInfo(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

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
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

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
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, result)
}
