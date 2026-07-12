package backup

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tech-arch1tect/berth-agent/internal/common"
	"github.com/tech-arch1tect/berth-agent/internal/validation"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type ListBackupsResponse struct {
	Configured bool         `json:"configured"`
	Total      int          `json:"total"`
	Runs       []RunSummary `json:"runs"`
}

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

func parseListPagination(c echo.Context) (limit, offset int) {
	limit = defaultListLimit
	if raw := c.QueryParam("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 1 {
			limit = min(parsed, maxListLimit)
		}
	}
	if raw := c.QueryParam("offset"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	return limit, offset
}

func (h *Handler) ListStackBackups(c echo.Context) error {
	stackName := c.Param("stackName")
	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "Invalid stack name: "+err.Error())
	}

	limit, offset := parseListPagination(c)
	total, summaries, err := h.service.ListRunSummaries(stackName, limit, offset)
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}

	return common.SendSuccess(c, ListBackupsResponse{
		Configured: h.service.Configured(),
		Total:      total,
		Runs:       summaries,
	})
}

func (h *Handler) GetStackBackup(c echo.Context) error {
	stackName := c.Param("stackName")
	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "Invalid stack name: "+err.Error())
	}
	backupID := c.Param("backupId")
	if backupID == "" {
		return common.SendBadRequest(c, "Backup id is required")
	}

	run, err := h.service.GetRun(stackName, backupID)
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}
	if run == nil {
		return common.SendNotFound(c, "backup not found")
	}
	return common.SendSuccess(c, run)
}

func (h *Handler) DeleteStackBackup(c echo.Context) error {
	stackName := c.Param("stackName")
	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "Invalid stack name: "+err.Error())
	}
	backupID := c.Param("backupId")
	if _, err := uuid.Parse(backupID); err != nil {
		return common.SendBadRequest(c, "Invalid backup id")
	}

	err := h.service.DeleteBackup(c.Request().Context(), stackName, backupID)
	switch {
	case err == nil:
		return common.SendMessage(c, "backup deleted")
	case errors.Is(err, ErrRunNotFound):
		return common.SendNotFound(c, "backup not found")
	case errors.Is(err, ErrRepositoryBusy):
		return common.SendConflict(c, err.Error())
	default:
		return common.SendInternalError(c, err.Error())
	}
}

func (s *Service) ListRunSummaries(stackName string, limit, offset int) (int, []RunSummary, error) {
	if err := validation.ValidateStackName(stackName); err != nil {
		return 0, nil, err
	}
	loaded, err := s.persistence.LoadStackRuns(stackName)
	if err != nil {
		return 0, nil, err
	}

	summaries := make([]RunSummary, 0, len(loaded))
	for _, run := range loaded {
		summaries = append(summaries, SummariseRun(run))
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].StartedAt.After(summaries[j].StartedAt)
	})

	total := len(summaries)
	if offset >= total {
		return total, []RunSummary{}, nil
	}
	end := min(offset+limit, total)
	return total, summaries[offset:end], nil
}

func (s *Service) GetRun(stackName, runID string) (*Run, error) {
	if err := validation.ValidateStackName(stackName); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(runID); err != nil {
		return nil, fmt.Errorf("invalid backup id %q", runID)
	}
	return s.persistence.LoadRun(stackName, runID)
}
