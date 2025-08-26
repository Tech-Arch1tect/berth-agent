package stats

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	statsService *Service
}

func NewHandler(statsService *Service) *Handler {
	return &Handler{
		statsService: statsService,
	}
}

func (h *Handler) GetStackStats(c echo.Context) error {
	stackName := c.Param("name")
	if stackName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "stack name is required"})
	}

	stats, err := h.statsService.GetStackStats(stackName)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, stats)
}
