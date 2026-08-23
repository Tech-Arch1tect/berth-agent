package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	if errors.Is(err, ErrRepositoryBusy) {
		return common.SendConflict(c, err.Error())
	}
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}

	return common.SendSuccess(c, ListBackupsResponse{
		Configured: h.service.Configured(),
		Total:      total,
		Runs:       summaries,
	})
}

func (h *Handler) GetBackupsOverview(c echo.Context) error {
	overview, err := h.service.BuildOverview()
	if errors.Is(err, ErrRepositoryBusy) {
		return common.SendConflict(c, err.Error())
	}
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}
	return common.SendSuccess(c, overview)
}

func (h *Handler) GetBackupHistory(c echo.Context) error {
	state, err := h.service.BackupHistory()
	if errors.Is(err, ErrRepositoryBusy) {
		return common.SendConflict(c, err.Error())
	}
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}
	return common.SendSuccess(c, state)
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
	if errors.Is(err, ErrRepositoryBusy) {
		return common.SendConflict(c, err.Error())
	}
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}
	if run == nil {
		return common.SendNotFound(c, "backup not found")
	}
	return common.SendSuccess(c, run)
}

type DeleteBackupRequest struct {
	BackupPassword string        `json:"backup_password"`
	Repository     *S3Repository `json:"s3_repository,omitempty"`
}

type DeleteAllBackupsRequest struct {
	BackupPassword   string        `json:"backup_password"`
	S3RepositoryBase *S3Repository `json:"s3_repository_base,omitempty"`
}

type RebuildRequest struct {
	BackupPassword string        `json:"backup_password"`
	Repository     *S3Repository `json:"s3_repository,omitempty"`
}

const backupPasswordHeader = "X-Backup-Password"
const backupS3RepositoryHeader = "X-Backup-S3-Repository"

func s3RepositoryFromHeader(c echo.Context) (*S3Repository, error) {
	raw := c.Request().Header.Get(backupS3RepositoryHeader)
	if raw == "" {
		return nil, nil
	}
	var s3Repo S3Repository
	if err := json.Unmarshal([]byte(raw), &s3Repo); err != nil {
		return nil, common.SendBadRequest(c, "The X-Backup-S3-Repository header could not be read")
	}
	if err := s3Repo.Validate(); err != nil {
		return nil, common.SendBadRequest(c, err.Error())
	}
	return &s3Repo, nil
}

func s3RepositoryFromBody(c echo.Context, s3Repo *S3Repository) (*S3Repository, error) {
	if s3Repo == nil {
		return nil, nil
	}
	if err := s3Repo.Validate(); err != nil {
		return nil, common.SendBadRequest(c, err.Error())
	}
	return s3Repo, nil
}

func (h *Handler) bindBrowseParams(c echo.Context) (stackName, backupID, componentID, password string, s3Repo *S3Repository, err error) {
	stackName = c.Param("stackName")
	if err := validation.ValidateStackName(stackName); err != nil {
		return "", "", "", "", nil, common.SendBadRequest(c, "Invalid stack name: "+err.Error())
	}
	backupID = c.Param("backupId")
	if _, err := uuid.Parse(backupID); err != nil {
		return "", "", "", "", nil, common.SendBadRequest(c, "Invalid backup id")
	}
	componentID = c.QueryParam("component")
	if componentID == "" {
		return "", "", "", "", nil, common.SendBadRequest(c, "A component id is required")
	}
	password = c.Request().Header.Get(backupPasswordHeader)
	if password == "" {
		return "", "", "", "", nil, common.SendBadRequest(c, "A backup password is required")
	}
	s3Repo, err = s3RepositoryFromHeader(c)
	if err != nil {
		return "", "", "", "", nil, err
	}
	return stackName, backupID, componentID, password, s3Repo, nil
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
	stackName, backupID, componentID, password, s3Repo, err := h.bindBrowseParams(c)
	if err != nil {
		return err
	}

	listing, svcErr := h.service.ListBackupFiles(c.Request().Context(), stackName, backupID, componentID, c.QueryParam("path"), password, s3Repo)
	if svcErr != nil {
		return h.sendBrowseError(c, svcErr)
	}
	return common.SendSuccess(c, listing)
}

func (h *Handler) DownloadBackupFiles(c echo.Context) error {
	stackName, backupID, componentID, password, s3Repo, err := h.bindBrowseParams(c)
	if err != nil {
		return err
	}
	paths := c.QueryParams()["path"]
	if len(paths) == 0 {
		return common.SendBadRequest(c, "At least one path is required")
	}

	releaseHistory, reserveErr := h.service.reserveHistoryRead()
	if reserveErr != nil {
		return h.sendBrowseError(c, reserveErr)
	}
	defer releaseHistory()
	releaseRepository, reserveErr := h.service.reserveRepositoryRead(stackName)
	if reserveErr != nil {
		return h.sendBrowseError(c, reserveErr)
	}
	defer releaseRepository()

	ctx := c.Request().Context()
	response := c.Response()

	entries, statErr := h.service.statBackupFiles(ctx, stackName, backupID, componentID, paths, password, s3Repo)
	if statErr != nil {
		return h.sendBrowseError(c, statErr)
	}

	if len(paths) == 1 && entries[0].Type == "file" {
		response.Header().Set(echo.HeaderContentType, "application/octet-stream")
		response.Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", entries[0].Name))
		response.Header().Set(echo.HeaderContentLength, strconv.FormatUint(entries[0].Size, 10))
		response.WriteHeader(http.StatusOK)
		return h.service.dumpBackupFile(ctx, stackName, backupID, componentID, paths[0], password, s3Repo, response.Writer)
	}

	response.Header().Set(echo.HeaderContentType, "application/gzip")
	response.Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", archiveFileName(stackName, backupID, componentID)))
	response.WriteHeader(http.StatusOK)
	return h.service.archiveBackupFiles(ctx, stackName, backupID, componentID, paths, password, s3Repo, response.Writer)
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

func (h *Handler) DeleteAllBackups(c echo.Context) error {
	var req DeleteAllBackupsRequest
	decoder := json.NewDecoder(c.Request().Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return common.SendBadRequest(c, "Invalid request body")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return common.SendBadRequest(c, "Invalid request body")
	}
	if req.BackupPassword == "" {
		return common.SendBadRequest(c, "A backup password is required to delete all backups")
	}
	if err := validateS3RepositoryBase(req.S3RepositoryBase); err != nil {
		return common.SendBadRequest(c, err.Error())
	}

	result, err := h.service.DeleteAllBackups(c.Request().Context(), req.BackupPassword, req.S3RepositoryBase)
	if errors.Is(err, ErrRepositoryBusy) {
		return common.SendConflict(c, err.Error())
	}
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}
	return common.SendSuccess(c, result)
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

	s3Repo, err := s3RepositoryFromBody(c, req.Repository)
	if err != nil {
		return err
	}

	err = h.service.DeleteBackup(c.Request().Context(), stackName, backupID, req.BackupPassword, s3Repo)
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

func (h *Handler) RebuildStackIndex(c echo.Context) error {
	stackName := c.Param("stackName")
	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "Invalid stack name: "+err.Error())
	}

	var req RebuildRequest
	if err := c.Bind(&req); err != nil {
		return common.SendBadRequest(c, "Invalid request body")
	}
	if req.BackupPassword == "" {
		return common.SendBadRequest(c, "A backup password is required to rebuild the backup history")
	}

	s3Repo, err := s3RepositoryFromBody(c, req.Repository)
	if err != nil {
		return err
	}

	writer := &bufferedProgressWriter{}
	result, err := h.service.RebuildIndex(c.Request().Context(), stackName, req.BackupPassword, s3Repo, writer)
	if errors.Is(err, ErrRepositoryBusy) {
		return common.SendConflict(c, err.Error())
	}
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}
	result.Output = writer.lines
	return common.SendSuccess(c, result)
}

func (s *Service) ListRunSummaries(stackName string, limit, offset int) (int, []RunSummary, error) {
	release, err := s.reserveHistoryRead()
	if err != nil {
		return 0, nil, err
	}
	defer release()

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
	release, err := s.reserveHistoryRead()
	if err != nil {
		return nil, err
	}
	defer release()
	return s.getRun(stackName, runID)
}

func (s *Service) getRun(stackName, runID string) (*Run, error) {
	if err := validation.ValidateStackName(stackName); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(runID); err != nil {
		return nil, fmt.Errorf("invalid backup id %q", runID)
	}
	return s.persistence.LoadRun(stackName, runID)
}
