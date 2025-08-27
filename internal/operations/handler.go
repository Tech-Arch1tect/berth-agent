package operations

import (
	"berth-agent/internal/validation"
	"net/http"
	"regexp"

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

func (h *Handler) StartOperation(c echo.Context) error {
	stackName := c.Param("stackName")
	if stackName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Stack name is required",
		})
	}

	if err := validation.ValidateStackName(stackName); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid stack name: " + err.Error(),
		})
	}

	var req OperationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid request format",
		})
	}

	if err := ValidateOperationRequest(req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid operation request: " + err.Error(),
		})
	}

	operationID, err := h.service.StartOperation(c.Request().Context(), stackName, req)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, OperationResponse{
		OperationID: operationID,
	})
}

func (h *Handler) StreamOperation(c echo.Context) error {
	operationID := c.Param("operationId")
	if operationID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Operation ID is required",
		})
	}

	if err := validateOperationID(operationID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid operation ID format",
		})
	}

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().Header().Set("Transfer-Encoding", "chunked")

	c.Response().Header().Del("Content-Length")

	c.Response().WriteHeader(http.StatusOK)

	if flusher, ok := c.Response().Writer.(http.Flusher); ok {
		flusher.Flush()
	}

	return h.service.StreamOperation(c.Request().Context(), operationID, c.Response().Writer)
}

func (h *Handler) GetOperationStatus(c echo.Context) error {
	operationID := c.Param("operationId")
	if operationID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Operation ID is required",
		})
	}

	if err := validateOperationID(operationID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid operation ID format",
		})
	}

	operation, exists := h.service.GetOperation(operationID)
	if !exists {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "Operation not found",
		})
	}

	return c.JSON(http.StatusOK, operation)
}

func validateOperationID(operationID string) error {

	uuidRegex := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	if !uuidRegex.MatchString(operationID) {
		return validation.ErrInvalidCharacters
	}
	return nil
}
