package logs

import (
	"net/http"
	"strconv"

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

type LogsResponse struct {
	Logs []LogEntry `json:"logs"`
}

func (h *Handler) GetStackLogs(c echo.Context) error {
	stackName := c.Param("stackName")
	if stackName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Stack name is required",
		})
	}

	req := LogRequest{
		StackName:  stackName,
		Tail:       h.parseIntParam(c, "tail", 100),
		Since:      c.QueryParam("since"),
		Timestamps: h.parseBoolParam(c, "timestamps", true),
	}

	logs, err := h.service.GetStackLogs(c.Request().Context(), req)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, LogsResponse{
		Logs: logs,
	})
}

func (h *Handler) GetContainerLogs(c echo.Context) error {
	containerName := c.Param("containerName")

	if containerName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Container name is required",
		})
	}

	req := LogRequest{
		ContainerName: containerName,
		Tail:          h.parseIntParam(c, "tail", 100),
		Since:         c.QueryParam("since"),
		Timestamps:    h.parseBoolParam(c, "timestamps", true),
	}

	logs, err := h.service.GetContainerLogs(c.Request().Context(), req)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, LogsResponse{
		Logs: logs,
	})
}

func (h *Handler) parseIntParam(c echo.Context, param string, defaultValue int) int {
	if value := c.QueryParam(param); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultValue
}

func (h *Handler) parseBoolParam(c echo.Context, param string, defaultValue bool) bool {
	if value := c.QueryParam(param); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}
