package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

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
	if err := validateRunID(backupID); err != nil {
		return common.SendBadRequest(c, "Invalid backup id")
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

type BrowseRequest struct {
	BackupPassword string        `json:"backup_password"`
	Repository     *S3Repository `json:"s3_repository,omitempty"`
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

func decodeBrowseRepository(decoder *json.Decoder) (*S3Repository, error) {
	token, err := decoder.Token()
	if err != nil || token == nil || token != json.Delim('{') {
		return nil, errors.New("Invalid request body")
	}

	repository := &S3Repository{}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, errors.New("Invalid request body")
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return nil, errors.New("Invalid request body")
		}
		seen[name] = true
		switch name {
		case "url":
			err = decoder.Decode(&repository.URL)
		case "access_key_id":
			err = decoder.Decode(&repository.AccessKeyID)
		case "secret_access_key":
			err = decoder.Decode(&repository.SecretKey)
		case "region":
			err = decoder.Decode(&repository.Region)
		default:
			return nil, errors.New("Invalid request body")
		}
		if err != nil {
			return nil, errors.New("Invalid request body")
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("Invalid request body")
	}
	return repository, nil
}

func decodeBrowseRequest(body io.Reader) (BrowseRequest, error) {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return BrowseRequest{}, errors.New("Invalid request body")
	}

	var request BrowseRequest
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return BrowseRequest{}, errors.New("Invalid request body")
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return BrowseRequest{}, errors.New("Invalid request body")
		}
		seen[name] = true
		switch name {
		case "backup_password":
			err = decoder.Decode(&request.BackupPassword)
		case "s3_repository":
			request.Repository, err = decodeBrowseRepository(decoder)
		default:
			return BrowseRequest{}, errors.New("Invalid request body")
		}
		if err != nil {
			return BrowseRequest{}, errors.New("Invalid request body")
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return BrowseRequest{}, errors.New("Invalid request body")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return BrowseRequest{}, errors.New("Invalid request body")
	}
	return request, nil
}

func (h *Handler) bindBrowseParams(c echo.Context) (stackName, backupID, componentID, password string, s3Repo *S3Repository, err error) {
	stackName = c.Param("stackName")
	if err := validation.ValidateStackName(stackName); err != nil {
		return "", "", "", "", nil, fmt.Errorf("Invalid stack name: %s", err)
	}
	backupID = c.Param("backupId")
	if err := validateRunID(backupID); err != nil {
		return "", "", "", "", nil, errors.New("Invalid backup id")
	}
	componentID = c.QueryParam("component")
	if componentID == "" {
		return "", "", "", "", nil, errors.New("A component id is required")
	}

	req, err := decodeBrowseRequest(c.Request().Body)
	if err != nil {
		return "", "", "", "", nil, err
	}
	if req.BackupPassword == "" {
		return "", "", "", "", nil, errors.New("A backup password is required")
	}
	if err := req.Repository.Validate(); err != nil {
		return "", "", "", "", nil, err
	}
	return stackName, backupID, componentID, req.BackupPassword, req.Repository, nil
}

func (h *Handler) sendBrowseError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, ErrRunNotFound):
		return common.SendNotFound(c, "backup not found")
	case errors.Is(err, ErrComponentNotFound), errors.Is(err, ErrPathNotFound):
		return common.SendNotFound(c, err.Error())
	case errors.Is(err, ErrRepositoryBusy):
		return common.SendConflict(c, err.Error())
	case errors.Is(err, errBackupRepositoryMissing), errors.Is(err, errBackupPasswordRejected):
		return common.SendInternalError(c, err.Error())
	default:
		return common.SendInternalError(c, "backup files could not be accessed")
	}
}

func (h *Handler) ListBackupFiles(c echo.Context) error {
	stackName, backupID, componentID, password, s3Repo, err := h.bindBrowseParams(c)
	if err != nil {
		return common.SendBadRequest(c, err.Error())
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
		return common.SendBadRequest(c, err.Error())
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

func (h *Handler) AbandonBackupStorage(c echo.Context) error {
	var body [1]byte
	read, err := c.Request().Body.Read(body[:])
	if read != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return common.SendBadRequest(c, "Request body must be empty")
	}

	result, err := h.service.AbandonBackupStorage(c.Request().Context())
	if errors.Is(err, ErrRepositoryBusy) {
		return common.SendConflict(c, err.Error())
	}
	if err != nil {
		return common.SendInternalError(c, "backup metadata could not be abandoned")
	}
	return common.SendSuccess(c, result)
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
	if err := validateRunID(backupID); err != nil {
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
	if err := validateRunID(runID); err != nil {
		return nil, fmt.Errorf("invalid backup id %q", runID)
	}
	return s.persistence.LoadRun(stackName, runID)
}
