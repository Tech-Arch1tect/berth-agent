package files

import (
	"berth-agent/internal/audit"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	service      *Service
	auditService *audit.Service
}

func NewHandler(service *Service, auditService *audit.Service) *Handler {
	return &Handler{
		service:      service,
		auditService: auditService,
	}
}

func (h *Handler) ListDirectory(c echo.Context) error {
	stackName := c.Param("stackName")
	path := c.QueryParam("path")

	result, err := h.service.ListDirectory(stackName, path)
	if err != nil {
		h.auditService.LogFileEvent(audit.EventFileListDir, c.RealIP(), stackName, path, false, err.Error(), nil)
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "LIST_DIRECTORY_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileListDir, c.RealIP(), stackName, path, true, "", map[string]any{
		"entry_count": len(result.Entries),
	})

	return c.JSON(http.StatusOK, result)
}

func (h *Handler) ReadFile(c echo.Context) error {
	stackName := c.Param("stackName")
	path := c.QueryParam("path")

	if path == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "path parameter is required",
			Code:  "MISSING_PATH",
		})
	}

	result, err := h.service.ReadFile(stackName, path)
	if err != nil {
		h.auditService.LogFileEvent(audit.EventFileRead, c.RealIP(), stackName, path, false, err.Error(), nil)
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "READ_FILE_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileRead, c.RealIP(), stackName, path, true, "", map[string]any{
		"size": result.Size,
	})

	return c.JSON(http.StatusOK, result)
}

func (h *Handler) WriteFile(c echo.Context) error {
	stackName := c.Param("stackName")

	var req WriteFileRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request body",
			Code:  "INVALID_REQUEST",
		})
	}

	fmt.Printf("DEBUG WriteFile: req.Path='%s', req.Content len=%d, req.Encoding='%s'\n", req.Path, len(req.Content), req.Encoding)

	if req.Path == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "path is required",
			Code:  "MISSING_PATH",
		})
	}

	if err := h.service.WriteFile(stackName, req); err != nil {
		h.auditService.LogFileEvent(audit.EventFileWrite, c.RealIP(), stackName, req.Path, false, err.Error(), nil)
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "WRITE_FILE_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileWrite, c.RealIP(), stackName, req.Path, true, "", map[string]any{
		"content_size": len(req.Content),
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}

func (h *Handler) CreateDirectory(c echo.Context) error {
	stackName := c.Param("stackName")

	var req CreateDirectoryRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request body",
			Code:  "INVALID_REQUEST",
		})
	}

	if req.Path == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "path is required",
			Code:  "MISSING_PATH",
		})
	}

	if err := h.service.CreateDirectory(stackName, req); err != nil {
		h.auditService.LogFileEvent(audit.EventFileMkdir, c.RealIP(), stackName, req.Path, false, err.Error(), nil)
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "CREATE_DIRECTORY_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileMkdir, c.RealIP(), stackName, req.Path, true, "", nil)

	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}

func (h *Handler) Delete(c echo.Context) error {
	stackName := c.Param("stackName")

	var req DeleteRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request body",
			Code:  "INVALID_REQUEST",
		})
	}

	if req.Path == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "path is required",
			Code:  "MISSING_PATH",
		})
	}

	if err := h.service.Delete(stackName, req); err != nil {
		h.auditService.LogFileEvent(audit.EventFileDelete, c.RealIP(), stackName, req.Path, false, err.Error(), nil)
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "DELETE_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileDelete, c.RealIP(), stackName, req.Path, true, "", nil)

	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}

func (h *Handler) Rename(c echo.Context) error {
	stackName := c.Param("stackName")

	var req RenameRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request body",
			Code:  "INVALID_REQUEST",
		})
	}

	if req.OldPath == "" || req.NewPath == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "old_path and new_path are required",
			Code:  "MISSING_PATHS",
		})
	}

	if err := h.service.Rename(stackName, req); err != nil {
		h.auditService.LogFileEvent(audit.EventFileRename, c.RealIP(), stackName, req.OldPath, false, err.Error(), map[string]any{
			"new_path": req.NewPath,
		})
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "RENAME_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileRename, c.RealIP(), stackName, req.OldPath, true, "", map[string]any{
		"new_path": req.NewPath,
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}

func (h *Handler) Copy(c echo.Context) error {
	stackName := c.Param("stackName")

	var req CopyRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request body",
			Code:  "INVALID_REQUEST",
		})
	}

	if req.SourcePath == "" || req.TargetPath == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "source_path and target_path are required",
			Code:  "MISSING_PATHS",
		})
	}

	if err := h.service.Copy(stackName, req); err != nil {
		h.auditService.LogFileEvent(audit.EventFileCopy, c.RealIP(), stackName, req.SourcePath, false, err.Error(), map[string]any{
			"target_path": req.TargetPath,
		})
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "COPY_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileCopy, c.RealIP(), stackName, req.SourcePath, true, "", map[string]any{
		"target_path": req.TargetPath,
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}

func (h *Handler) UploadFile(c echo.Context) error {
	stackName := c.Param("stackName")
	path := c.FormValue("path")

	if path == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "path parameter is required",
			Code:  "MISSING_PATH",
		})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "file parameter is required",
			Code:  "MISSING_FILE",
		})
	}

	var mode *string
	var ownerID *uint32
	var groupID *uint32

	if modeStr := c.FormValue("mode"); modeStr != "" {
		mode = &modeStr
	}

	if ownerStr := c.FormValue("owner_id"); ownerStr != "" {
		if parsed, err := strconv.ParseUint(ownerStr, 10, 32); err == nil {
			uid := uint32(parsed)
			ownerID = &uid
		}
	}

	if groupStr := c.FormValue("group_id"); groupStr != "" {
		if parsed, err := strconv.ParseUint(groupStr, 10, 32); err == nil {
			gid := uint32(parsed)
			groupID = &gid
		}
	}

	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "cannot open uploaded file",
			Code:  "OPEN_FILE_ERROR",
		})
	}
	defer src.Close()

	if err := h.service.WriteUploadedFile(stackName, path, src, file.Size, mode, ownerID, groupID); err != nil {
		h.auditService.LogFileEvent(audit.EventFileUpload, c.RealIP(), stackName, path, false, err.Error(), map[string]any{
			"filename": file.Filename,
			"size":     file.Size,
		})
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "UPLOAD_FILE_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileUpload, c.RealIP(), stackName, path, true, "", map[string]any{
		"filename": file.Filename,
		"size":     file.Size,
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}

func (h *Handler) DownloadFile(c echo.Context) error {
	stackName := c.Param("stackName")
	path := c.QueryParam("path")

	if path == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "path parameter is required",
			Code:  "MISSING_PATH",
		})
	}

	fileContent, err := h.service.ReadFile(stackName, path)
	if err != nil {
		h.auditService.LogFileEvent(audit.EventFileDownload, c.RealIP(), stackName, path, false, err.Error(), nil)
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "READ_FILE_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileDownload, c.RealIP(), stackName, path, true, "", map[string]any{
		"size": fileContent.Size,
	})

	filename := c.QueryParam("filename")
	if filename == "" {
		filename = fileContent.Path
		if idx := strings.LastIndex(filename, "/"); idx >= 0 {
			filename = filename[idx+1:]
		}
	}

	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Response().Header().Set("Content-Length", strconv.FormatInt(fileContent.Size, 10))

	if fileContent.Encoding == "base64" {
		c.Response().Header().Set("Content-Type", "application/octet-stream")
		decoded, err := base64.StdEncoding.DecodeString(fileContent.Content)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error: "Failed to decode file content",
				Code:  "DECODE_ERROR",
			})
		}
		return c.Blob(http.StatusOK, "application/octet-stream", decoded)
	} else {
		c.Response().Header().Set("Content-Type", "text/plain")
		return c.String(http.StatusOK, fileContent.Content)
	}
}

func (h *Handler) GetDirectoryStats(c echo.Context) error {
	stackName := c.Param("stackName")
	path := c.QueryParam("path")

	if path == "" {
		path = "."
	}

	req := DirectoryStatsRequest{Path: path}
	stats, err := h.service.GetDirectoryStats(stackName, req)
	if err != nil {
		h.auditService.LogFileEvent(audit.EventFileDirStats, c.RealIP(), stackName, path, false, err.Error(), nil)
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "DIRECTORY_STATS_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileDirStats, c.RealIP(), stackName, path, true, "", nil)

	return c.JSON(http.StatusOK, stats)
}

func (h *Handler) Chmod(c echo.Context) error {
	stackName := c.Param("stackName")

	var req ChmodRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request body",
			Code:  "INVALID_REQUEST",
		})
	}

	if req.Path == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "path is required",
			Code:  "MISSING_PATH",
		})
	}

	if req.Mode == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "mode is required",
			Code:  "MISSING_MODE",
		})
	}

	if err := h.service.Chmod(stackName, req); err != nil {
		h.auditService.LogFileEvent(audit.EventFileChmod, c.RealIP(), stackName, req.Path, false, err.Error(), map[string]any{
			"mode":      req.Mode,
			"recursive": req.Recursive,
		})
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "CHMOD_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileChmod, c.RealIP(), stackName, req.Path, true, "", map[string]any{
		"mode":      req.Mode,
		"recursive": req.Recursive,
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}

func (h *Handler) Chown(c echo.Context) error {
	stackName := c.Param("stackName")

	var req ChownRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request body",
			Code:  "INVALID_REQUEST",
		})
	}

	if req.Path == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "path is required",
			Code:  "MISSING_PATH",
		})
	}

	if req.OwnerID == nil && req.GroupID == nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "owner_id or group_id is required",
			Code:  "MISSING_OWNER_GROUP",
		})
	}

	if err := h.service.Chown(stackName, req); err != nil {
		h.auditService.LogFileEvent(audit.EventFileChown, c.RealIP(), stackName, req.Path, false, err.Error(), map[string]any{
			"owner_id":  req.OwnerID,
			"group_id":  req.GroupID,
			"recursive": req.Recursive,
		})
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "CHOWN_ERROR",
		})
	}

	h.auditService.LogFileEvent(audit.EventFileChown, c.RealIP(), stackName, req.Path, true, "", map[string]any{
		"owner_id":  req.OwnerID,
		"group_id":  req.GroupID,
		"recursive": req.Recursive,
	})

	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}
