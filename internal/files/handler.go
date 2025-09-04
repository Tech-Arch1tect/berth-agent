package files

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

func (h *Handler) ListDirectory(c echo.Context) error {
	stackName := c.Param("stackName")
	path := c.QueryParam("path")

	result, err := h.service.ListDirectory(stackName, path)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "LIST_DIRECTORY_ERROR",
		})
	}

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
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "READ_FILE_ERROR",
		})
	}

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
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "WRITE_FILE_ERROR",
		})
	}

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
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "CREATE_DIRECTORY_ERROR",
		})
	}

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
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "DELETE_ERROR",
		})
	}

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
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "RENAME_ERROR",
		})
	}

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
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "COPY_ERROR",
		})
	}

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

	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "cannot open uploaded file",
			Code:  "OPEN_FILE_ERROR",
		})
	}
	defer src.Close()

	if err := h.service.WriteUploadedFile(stackName, path, src, file.Size); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "UPLOAD_FILE_ERROR",
		})
	}

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
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "READ_FILE_ERROR",
		})
	}

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
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "CHMOD_ERROR",
		})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}
