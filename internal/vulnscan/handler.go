package vulnscan

import (
	"berth-agent/internal/common"
	"berth-agent/internal/validation"
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

func (h *Handler) StartScan(c echo.Context) error {
	stackName := c.Param("stackName")
	if stackName == "" {
		return common.SendBadRequest(c, "Stack name is required")
	}

	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "Invalid stack name: "+err.Error())
	}

	scan, err := h.service.StartScan(c.Request().Context(), stackName)
	if err != nil {
		return common.SendBadRequest(c, err.Error())
	}

	return common.SendSuccess(c, GetScanResponse{
		ID:            scan.ID,
		StackName:     scan.StackName,
		Status:        scan.Status,
		TotalImages:   scan.TotalImages,
		ScannedImages: scan.ScannedImages,
		StartedAt:     scan.StartedAt,
		CompletedAt:   scan.CompletedAt,
		Error:         scan.Error,
		Results:       scan.Results,
	})
}

func (h *Handler) GetScan(c echo.Context) error {
	scanID := c.Param("scanId")
	if scanID == "" {
		return common.SendBadRequest(c, "Scan ID is required")
	}

	if err := validateScanID(scanID); err != nil {
		return common.SendBadRequest(c, "Invalid scan ID format")
	}

	scan, exists := h.service.GetScan(scanID)
	if !exists {
		return common.SendNotFound(c, "Scan not found")
	}

	return common.SendSuccess(c, GetScanResponse{
		ID:            scan.ID,
		StackName:     scan.StackName,
		Status:        scan.Status,
		TotalImages:   scan.TotalImages,
		ScannedImages: scan.ScannedImages,
		StartedAt:     scan.StartedAt,
		CompletedAt:   scan.CompletedAt,
		Error:         scan.Error,
		Results:       scan.Results,
	})
}

func (h *Handler) GetScannerStatus(c echo.Context) error {
	return common.SendSuccess(c, map[string]bool{
		"available": h.service.IsAvailable(),
	})
}

func validateScanID(scanID string) error {
	uuidRegex := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	if !uuidRegex.MatchString(scanID) {
		return validation.ErrInvalidCharacters
	}
	return nil
}
