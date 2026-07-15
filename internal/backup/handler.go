package backup

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

type DeleteBackupRequest struct {
	BackupPassword string `json:"backup_password"`
}

const backupPasswordHeader = "X-Backup-Password"

func (h *Handler) bindBrowseParams(c echo.Context) (stackName, backupID, componentID, password string, err error) {
	stackName = c.Param("stackName")
	if err := validation.ValidateStackName(stackName); err != nil {
		return "", "", "", "", common.SendBadRequest(c, "Invalid stack name: "+err.Error())
	}
	backupID = c.Param("backupId")
	if _, err := uuid.Parse(backupID); err != nil {
		return "", "", "", "", common.SendBadRequest(c, "Invalid backup id")
	}
	componentID = c.QueryParam("component")
	if componentID == "" {
		return "", "", "", "", common.SendBadRequest(c, "A component id is required")
	}
	password = c.Request().Header.Get(backupPasswordHeader)
	if password == "" {
		return "", "", "", "", common.SendBadRequest(c, "A backup password is required")
	}
	return stackName, backupID, componentID, password, nil
}

func (h *Handler) sendBrowseError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, ErrRunNotFound):
		return common.SendNotFound(c, "backup not found")
	case errors.Is(err, ErrComponentNotFound), errors.Is(err, ErrPathNotFound):
		return common.SendNotFound(c, err.Error())
	case errors.Is(err, ErrRepositoryBusy):
		return common.SendConflict(c, err.Error())
	default:
		return common.SendInternalError(c, err.Error())
	}
}

func (h *Handler) ListBackupFiles(c echo.Context) error {
	stackName, backupID, componentID, password, err := h.bindBrowseParams(c)
	if err != nil {
		return err
	}

	listing, svcErr := h.service.ListBackupFiles(c.Request().Context(), stackName, backupID, componentID, c.QueryParam("path"), password)
	if svcErr != nil {
		return h.sendBrowseError(c, svcErr)
	}
	return common.SendSuccess(c, listing)
}

func (h *Handler) DownloadBackupFiles(c echo.Context) error {
	stackName, backupID, componentID, password, err := h.bindBrowseParams(c)
	if err != nil {
		return err
	}
	paths := c.QueryParams()["path"]
	if len(paths) == 0 {
		return common.SendBadRequest(c, "At least one path is required")
	}

	ctx := c.Request().Context()
	response := c.Response()

	if len(paths) == 1 {
		entry, statErr := h.service.StatBackupFile(ctx, stackName, backupID, componentID, paths[0], password)
		if statErr != nil {
			return h.sendBrowseError(c, statErr)
		}
		if entry.Type == "file" {
			response.Header().Set(echo.HeaderContentType, "application/octet-stream")
			response.Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", entry.Name))
			response.Header().Set(echo.HeaderContentLength, strconv.FormatUint(entry.Size, 10))
			response.WriteHeader(http.StatusOK)
			return h.service.DumpBackupFile(ctx, stackName, backupID, componentID, paths[0], password, response.Writer)
		}
	}

	response.Header().Set(echo.HeaderContentType, "application/gzip")
	response.Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", archiveFileName(stackName, backupID, componentID)))
	response.WriteHeader(http.StatusOK)
	return h.service.ArchiveBackupFiles(ctx, stackName, backupID, componentID, paths, password, response.Writer)
}

func archiveFileName(stackName, backupID, componentID string) string {
	sanitise := func(s string) string {
		return strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
				return r
			}
			return '-'
		}, s)
	}
	shortID := backupID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return sanitise(stackName) + "-" + sanitise(shortID) + "-" + sanitise(componentID) + ".tar.gz"
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

	var req DeleteBackupRequest
	if err := c.Bind(&req); err != nil {
		return common.SendBadRequest(c, "Invalid request body")
	}
	if req.BackupPassword == "" {
		return common.SendBadRequest(c, "A backup password is required to delete a backup")
	}

	err := h.service.DeleteBackup(c.Request().Context(), stackName, backupID, req.BackupPassword)
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
	summaries, err := s.persistence.RunSummaries(stackName)
	if err != nil {
		return 0, nil, err
	}

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
