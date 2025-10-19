package files

import (
	"berth-agent/config"
	"berth-agent/internal/logging"
	"berth-agent/internal/validation"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"go.uber.org/zap"
)

type Service struct {
	stackLocation string
	logger        *logging.Logger
}

func NewService(cfg *config.Config, logger *logging.Logger) *Service {
	return &Service{
		stackLocation: cfg.StackLocation,
		logger:        logger.With(zap.String("service", "files")),
	}
}

func (s *Service) validateStackPath(stackName, relativePath string) (string, error) {
	stackPath, err := validation.SanitizeStackPath(s.stackLocation, stackName)
	if err != nil {
		return "", fmt.Errorf("invalid stack name '%s': %w", stackName, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return "", fmt.Errorf("stack '%s' not found", stackName)
	}

	if relativePath == "" || relativePath == "/" {
		return stackPath, nil
	}

	relativePath = strings.TrimPrefix(relativePath, "/")

	cleanPath := filepath.Clean(relativePath)
	if strings.Contains(cleanPath, "..") {
		s.logger.Warn("path traversal attempt detected",
			zap.String("stack", stackName),
			zap.String("requested_path", relativePath),
			zap.String("clean_path", cleanPath),
		)
		return "", errors.New("path traversal not allowed")
	}

	fullPath := filepath.Join(stackPath, cleanPath)

	relativeCheck, err := filepath.Rel(stackPath, fullPath)
	if err != nil || strings.HasPrefix(relativeCheck, "..") {
		s.logger.Warn("path outside stack directory attempted",
			zap.String("stack", stackName),
			zap.String("requested_path", relativePath),
			zap.String("full_path", fullPath),
		)
		return "", errors.New("path outside stack directory")
	}

	return fullPath, nil
}

func (s *Service) ListDirectory(stackName, path string) (*DirectoryListing, error) {
	fullPath, err := s.validateStackPath(stackName, path)
	if err != nil {
		return nil, err
	}

	stat, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.logger.Error("directory not found",
				zap.String("operation", "list_directory"),
				zap.String("stack", stackName),
				zap.String("path", path),
				zap.String("full_path", fullPath),
			)
			return nil, fmt.Errorf("path not found: %s", path)
		}
		s.logger.Error("cannot access directory",
			zap.String("operation", "list_directory"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return nil, fmt.Errorf("cannot access path: %w", err)
	}

	if !stat.IsDir() {
		s.logger.Error("path is not a directory",
			zap.String("operation", "list_directory"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
		)
		return nil, fmt.Errorf("path is not a directory: %s", path)
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		s.logger.Error("cannot read directory",
			zap.String("operation", "list_directory"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return nil, fmt.Errorf("cannot read directory: %w", err)
	}

	s.logger.Debug("listing directory",
		zap.String("operation", "list_directory"),
		zap.String("stack", stackName),
		zap.String("path", path),
		zap.String("full_path", fullPath),
		zap.Int("entry_count", len(entries)),
	)

	var fileEntries []FileEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		relativePath := filepath.Join(path, entry.Name())
		if path == "" {
			relativePath = entry.Name()
		}

		fileEntry := FileEntry{
			Name:        entry.Name(),
			Path:        relativePath,
			Size:        info.Size(),
			IsDirectory: entry.IsDir(),
			ModTime:     info.ModTime(),
			Mode:        info.Mode().String(),
		}

		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			fileEntry.OwnerID = stat.Uid
			fileEntry.GroupID = stat.Gid

			if ownerName, err := getUserName(stat.Uid); err == nil {
				fileEntry.Owner = ownerName
			}

			if groupName, err := getGroupName(stat.Gid); err == nil {
				fileEntry.Group = groupName
			}
		}

		if !entry.IsDir() {
			ext := filepath.Ext(entry.Name())
			if ext != "" {
				fileEntry.Extension = strings.ToLower(ext[1:])
			}
		}

		fileEntries = append(fileEntries, fileEntry)
	}

	sort.Slice(fileEntries, func(i, j int) bool {
		if fileEntries[i].IsDirectory != fileEntries[j].IsDirectory {
			return fileEntries[i].IsDirectory
		}
		return fileEntries[i].Name < fileEntries[j].Name
	})

	return &DirectoryListing{
		Path:    path,
		Entries: fileEntries,
	}, nil
}

func (s *Service) ReadFile(stackName, path string) (*FileContent, error) {
	fullPath, err := s.validateStackPath(stackName, path)
	if err != nil {
		return nil, err
	}

	stat, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.logger.Error("file not found",
				zap.String("operation", "read_file"),
				zap.String("stack", stackName),
				zap.String("path", path),
				zap.String("full_path", fullPath),
			)
			return nil, fmt.Errorf("file not found: %s", path)
		}
		s.logger.Error("cannot access file",
			zap.String("operation", "read_file"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return nil, fmt.Errorf("cannot access file: %w", err)
	}

	if stat.IsDir() {
		s.logger.Error("path is a directory, not a file",
			zap.String("operation", "read_file"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
		)
		return nil, fmt.Errorf("path is a directory, not a file: %s", path)
	}

	if stat.Size() > 10*1024*1024 {
		s.logger.Error("file too large to read",
			zap.String("operation", "read_file"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
			zap.Int64("size", stat.Size()),
			zap.Int64("max_size", 10*1024*1024),
		)
		return nil, errors.New("file too large (>10MB)")
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		s.logger.Error("cannot read file",
			zap.String("operation", "read_file"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return nil, fmt.Errorf("cannot read file: %w", err)
	}

	encoding := "utf-8"
	contentStr := string(content)

	if !utf8.Valid(content) {
		encoding = "base64"
		contentStr = base64.StdEncoding.EncodeToString(content)
	}

	s.logger.Info("file read successfully",
		zap.String("operation", "read_file"),
		zap.String("stack", stackName),
		zap.String("path", path),
		zap.String("full_path", fullPath),
		zap.Int64("size", stat.Size()),
		zap.String("encoding", encoding),
	)

	return &FileContent{
		Path:     path,
		Content:  contentStr,
		Size:     stat.Size(),
		Encoding: encoding,
	}, nil
}

func (s *Service) WriteFile(stackName string, req WriteFileRequest) error {
	fullPath, err := s.validateStackPath(stackName, req.Path)
	if err != nil {
		return err
	}

	var content []byte
	if req.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			s.logger.Error("invalid base64 content",
				zap.String("operation", "write_file"),
				zap.String("stack", stackName),
				zap.String("path", req.Path),
				zap.Error(err),
			)
			return fmt.Errorf("invalid base64 content: %w", err)
		}
		content = decoded
	} else {
		content = []byte(req.Content)
	}

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		s.logger.Error("cannot create parent directory",
			zap.String("operation", "write_file"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("directory", dir),
			zap.Error(err),
		)
		return fmt.Errorf("cannot create directory: %w", err)
	}

	fileMode := os.FileMode(0644)
	if req.Mode != nil {
		parsedMode, err := strconv.ParseUint(*req.Mode, 8, 32)
		if err != nil {
			s.logger.Error("invalid file mode",
				zap.String("operation", "write_file"),
				zap.String("stack", stackName),
				zap.String("path", req.Path),
				zap.String("mode", *req.Mode),
				zap.Error(err),
			)
			return fmt.Errorf("invalid file mode: %w", err)
		}
		fileMode = os.FileMode(parsedMode)
	}

	tempPath := fullPath + ".tmp"
	if err := os.WriteFile(tempPath, content, fileMode); err != nil {
		s.logger.Error("cannot write file",
			zap.String("operation", "write_file"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
			zap.Int("size", len(content)),
			zap.Error(err),
		)
		return fmt.Errorf("cannot write file: %w", err)
	}

	if err := os.Rename(tempPath, fullPath); err != nil {
		_ = os.Remove(tempPath)
		s.logger.Error("cannot move file into place",
			zap.String("operation", "write_file"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot move file into place: %w", err)
	}

	if req.OwnerID != nil || req.GroupID != nil {
		uid := -1
		gid := -1

		if req.OwnerID != nil {
			uid = int(*req.OwnerID)
		}
		if req.GroupID != nil {
			gid = int(*req.GroupID)
		}

		if err := os.Chown(fullPath, uid, gid); err != nil {
			s.logger.Error("cannot change ownership",
				zap.String("operation", "write_file"),
				zap.String("stack", stackName),
				zap.String("path", req.Path),
				zap.String("full_path", fullPath),
				zap.Int("uid", uid),
				zap.Int("gid", gid),
				zap.Error(err),
			)
			return fmt.Errorf("cannot change ownership: %w", err)
		}
	}

	s.logger.Info("file written successfully",
		zap.String("operation", "write_file"),
		zap.String("stack", stackName),
		zap.String("path", req.Path),
		zap.String("full_path", fullPath),
		zap.Int("size", len(content)),
		zap.String("mode", fileMode.String()),
	)

	return nil
}

func (s *Service) CreateDirectory(stackName string, req CreateDirectoryRequest) error {
	fullPath, err := s.validateStackPath(stackName, req.Path)
	if err != nil {
		return err
	}

	dirMode := os.FileMode(0755)
	if req.Mode != nil {
		parsedMode, err := strconv.ParseUint(*req.Mode, 8, 32)
		if err != nil {
			s.logger.Error("invalid directory mode",
				zap.String("operation", "create_directory"),
				zap.String("stack", stackName),
				zap.String("path", req.Path),
				zap.String("mode", *req.Mode),
				zap.Error(err),
			)
			return fmt.Errorf("invalid directory mode: %w", err)
		}
		dirMode = os.FileMode(parsedMode)
	}

	if err := os.MkdirAll(fullPath, dirMode); err != nil {
		s.logger.Error("cannot create directory",
			zap.String("operation", "create_directory"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot create directory: %w", err)
	}

	if req.OwnerID != nil || req.GroupID != nil {
		uid := -1
		gid := -1

		if req.OwnerID != nil {
			uid = int(*req.OwnerID)
		}
		if req.GroupID != nil {
			gid = int(*req.GroupID)
		}

		if err := os.Chown(fullPath, uid, gid); err != nil {
			s.logger.Error("cannot change ownership",
				zap.String("operation", "create_directory"),
				zap.String("stack", stackName),
				zap.String("path", req.Path),
				zap.String("full_path", fullPath),
				zap.Int("uid", uid),
				zap.Int("gid", gid),
				zap.Error(err),
			)
			return fmt.Errorf("cannot change ownership: %w", err)
		}
	}

	s.logger.Info("directory created successfully",
		zap.String("operation", "create_directory"),
		zap.String("stack", stackName),
		zap.String("path", req.Path),
		zap.String("full_path", fullPath),
		zap.String("mode", dirMode.String()),
	)

	return nil
}

func (s *Service) GetDirectoryStats(stackName string, req DirectoryStatsRequest) (*DirectoryStats, error) {
	fullPath, err := s.validateStackPath(stackName, req.Path)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory: %w", err)
	}

	ownerCounts := make(map[uint32]int)
	groupCounts := make(map[uint32]int)
	modeCounts := make(map[string]int)

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}

		ownerCounts[stat.Uid]++
		groupCounts[stat.Gid]++
		modeCounts[fmt.Sprintf("%o", info.Mode().Perm())]++
	}

	var mostCommonOwner uint32
	var mostCommonGroup uint32
	var mostCommonMode string
	maxOwnerCount := 0
	maxGroupCount := 0
	maxModeCount := 0

	for uid, count := range ownerCounts {
		if count > maxOwnerCount {
			maxOwnerCount = count
			mostCommonOwner = uid
		}
	}

	for gid, count := range groupCounts {
		if count > maxGroupCount {
			maxGroupCount = count
			mostCommonGroup = gid
		}
	}

	for mode, count := range modeCounts {
		if count > maxModeCount {
			maxModeCount = count
			mostCommonMode = mode
		}
	}

	ownerName, _ := getUserName(mostCommonOwner)
	groupName, _ := getGroupName(mostCommonGroup)

	return &DirectoryStats{
		Path:            req.Path,
		MostCommonOwner: mostCommonOwner,
		MostCommonGroup: mostCommonGroup,
		MostCommonMode:  mostCommonMode,
		OwnerName:       ownerName,
		GroupName:       groupName,
	}, nil
}

func (s *Service) Delete(stackName string, req DeleteRequest) error {
	fullPath, err := s.validateStackPath(stackName, req.Path)
	if err != nil {
		return err
	}

	stat, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.logger.Debug("path already deleted",
				zap.String("operation", "delete"),
				zap.String("stack", stackName),
				zap.String("path", req.Path),
				zap.String("full_path", fullPath),
			)
			return nil
		}
		s.logger.Error("cannot access path for deletion",
			zap.String("operation", "delete"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot access path: %w", err)
	}

	isDirectory := stat.IsDir()

	if isDirectory {
		if err := os.RemoveAll(fullPath); err != nil {
			s.logger.Error("cannot delete directory",
				zap.String("operation", "delete"),
				zap.String("stack", stackName),
				zap.String("path", req.Path),
				zap.String("full_path", fullPath),
				zap.Error(err),
			)
			return fmt.Errorf("cannot delete directory: %w", err)
		}
	} else {
		if err := os.Remove(fullPath); err != nil {
			s.logger.Error("cannot delete file",
				zap.String("operation", "delete"),
				zap.String("stack", stackName),
				zap.String("path", req.Path),
				zap.String("full_path", fullPath),
				zap.Error(err),
			)
			return fmt.Errorf("cannot delete file: %w", err)
		}
	}

	s.logger.Info("deleted successfully",
		zap.String("operation", "delete"),
		zap.String("stack", stackName),
		zap.String("path", req.Path),
		zap.String("full_path", fullPath),
		zap.Bool("is_directory", isDirectory),
	)

	return nil
}

func (s *Service) Rename(stackName string, req RenameRequest) error {
	oldFullPath, err := s.validateStackPath(stackName, req.OldPath)
	if err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}

	newFullPath, err := s.validateStackPath(stackName, req.NewPath)
	if err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	if _, err := os.Stat(oldFullPath); os.IsNotExist(err) {
		s.logger.Error("source path not found for rename",
			zap.String("operation", "rename"),
			zap.String("stack", stackName),
			zap.String("old_path", req.OldPath),
			zap.String("new_path", req.NewPath),
		)
		return fmt.Errorf("source path not found: %s", req.OldPath)
	}

	if _, err := os.Stat(newFullPath); err == nil {
		s.logger.Error("destination path already exists",
			zap.String("operation", "rename"),
			zap.String("stack", stackName),
			zap.String("old_path", req.OldPath),
			zap.String("new_path", req.NewPath),
		)
		return fmt.Errorf("destination path already exists: %s", req.NewPath)
	}

	dir := filepath.Dir(newFullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		s.logger.Error("cannot create destination directory for rename",
			zap.String("operation", "rename"),
			zap.String("stack", stackName),
			zap.String("old_path", req.OldPath),
			zap.String("new_path", req.NewPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot create destination directory: %w", err)
	}

	if err := os.Rename(oldFullPath, newFullPath); err != nil {
		s.logger.Error("cannot rename path",
			zap.String("operation", "rename"),
			zap.String("stack", stackName),
			zap.String("old_path", req.OldPath),
			zap.String("new_path", req.NewPath),
			zap.String("old_full_path", oldFullPath),
			zap.String("new_full_path", newFullPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot rename: %w", err)
	}

	s.logger.Info("renamed successfully",
		zap.String("operation", "rename"),
		zap.String("stack", stackName),
		zap.String("old_path", req.OldPath),
		zap.String("new_path", req.NewPath),
		zap.String("old_full_path", oldFullPath),
		zap.String("new_full_path", newFullPath),
	)

	return nil
}

func (s *Service) Copy(stackName string, req CopyRequest) error {
	sourceFullPath, err := s.validateStackPath(stackName, req.SourcePath)
	if err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}

	targetFullPath, err := s.validateStackPath(stackName, req.TargetPath)
	if err != nil {
		return fmt.Errorf("invalid target path: %w", err)
	}

	sourceStat, err := os.Stat(sourceFullPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.logger.Error("source path not found for copy",
				zap.String("operation", "copy"),
				zap.String("stack", stackName),
				zap.String("source_path", req.SourcePath),
				zap.String("target_path", req.TargetPath),
			)
			return fmt.Errorf("source path not found: %s", req.SourcePath)
		}
		s.logger.Error("cannot access source path for copy",
			zap.String("operation", "copy"),
			zap.String("stack", stackName),
			zap.String("source_path", req.SourcePath),
			zap.String("target_path", req.TargetPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot access source path: %w", err)
	}

	if _, err := os.Stat(targetFullPath); err == nil {
		s.logger.Error("target path already exists",
			zap.String("operation", "copy"),
			zap.String("stack", stackName),
			zap.String("source_path", req.SourcePath),
			zap.String("target_path", req.TargetPath),
		)
		return fmt.Errorf("target path already exists: %s", req.TargetPath)
	}

	dir := filepath.Dir(targetFullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		s.logger.Error("cannot create target directory for copy",
			zap.String("operation", "copy"),
			zap.String("stack", stackName),
			zap.String("source_path", req.SourcePath),
			zap.String("target_path", req.TargetPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot create target directory: %w", err)
	}

	isDirectory := sourceStat.IsDir()

	var copyErr error
	if isDirectory {
		copyErr = s.copyDirectory(sourceFullPath, targetFullPath)
	} else {
		copyErr = s.copyFile(sourceFullPath, targetFullPath)
	}

	if copyErr != nil {
		s.logger.Error("cannot copy path",
			zap.String("operation", "copy"),
			zap.String("stack", stackName),
			zap.String("source_path", req.SourcePath),
			zap.String("target_path", req.TargetPath),
			zap.String("source_full_path", sourceFullPath),
			zap.String("target_full_path", targetFullPath),
			zap.Bool("is_directory", isDirectory),
			zap.Error(copyErr),
		)
		return copyErr
	}

	s.logger.Info("copied successfully",
		zap.String("operation", "copy"),
		zap.String("stack", stackName),
		zap.String("source_path", req.SourcePath),
		zap.String("target_path", req.TargetPath),
		zap.String("source_full_path", sourceFullPath),
		zap.String("target_full_path", targetFullPath),
		zap.Bool("is_directory", isDirectory),
	)

	return nil
}

func (s *Service) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("cannot open source file: %w", err)
	}
	defer func() { _ = sourceFile.Close() }()

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("cannot create destination file: %w", err)
	}
	defer func() { _ = destFile.Close() }()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("cannot copy file: %w", err)
	}

	sourceInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("cannot get source file info: %w", err)
	}

	if err := os.Chmod(dst, sourceInfo.Mode()); err != nil {
		return fmt.Errorf("cannot set file permissions: %w", err)
	}

	return nil
}

func (s *Service) copyDirectory(src, dst string) error {
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("cannot get source directory info: %w", err)
	}

	if err := os.MkdirAll(dst, sourceInfo.Mode()); err != nil {
		return fmt.Errorf("cannot create destination directory: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("cannot read source directory: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := s.copyDirectory(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := s.copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Service) WriteUploadedFile(stackName, path string, src io.Reader, size int64, mode *string, ownerID *uint32, groupID *uint32) error {
	fullPath, err := s.validateStackPath(stackName, path)
	if err != nil {
		return err
	}

	if size > 100*1024*1024 {
		s.logger.Error("uploaded file too large",
			zap.String("operation", "upload_file"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.Int64("size", size),
			zap.Int64("max_size", 100*1024*1024),
		)
		return errors.New("file too large (>100MB)")
	}

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		s.logger.Error("cannot create directory for upload",
			zap.String("operation", "upload_file"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("directory", dir),
			zap.Error(err),
		)
		return fmt.Errorf("cannot create directory: %w", err)
	}

	tempPath := fullPath + ".tmp"
	destFile, err := os.Create(tempPath)
	if err != nil {
		s.logger.Error("cannot create upload file",
			zap.String("operation", "upload_file"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot create file: %w", err)
	}
	defer func() { _ = destFile.Close() }()

	if _, err := io.Copy(destFile, src); err != nil {
		_ = os.Remove(tempPath)
		s.logger.Error("cannot write uploaded file",
			zap.String("operation", "upload_file"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot write file: %w", err)
	}

	if err := destFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		s.logger.Error("cannot close uploaded file",
			zap.String("operation", "upload_file"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot close file: %w", err)
	}

	if err := os.Rename(tempPath, fullPath); err != nil {
		_ = os.Remove(tempPath)
		s.logger.Error("cannot move uploaded file into place",
			zap.String("operation", "upload_file"),
			zap.String("stack", stackName),
			zap.String("path", path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot move file into place: %w", err)
	}

	if mode != nil {
		parsedMode, err := strconv.ParseUint(*mode, 8, 32)
		if err != nil {
			s.logger.Error("invalid file mode for upload",
				zap.String("operation", "upload_file"),
				zap.String("stack", stackName),
				zap.String("path", path),
				zap.String("mode", *mode),
				zap.Error(err),
			)
			return fmt.Errorf("invalid file mode: %w", err)
		}
		if err := os.Chmod(fullPath, os.FileMode(parsedMode)); err != nil {
			s.logger.Error("cannot set permissions on uploaded file",
				zap.String("operation", "upload_file"),
				zap.String("stack", stackName),
				zap.String("path", path),
				zap.String("full_path", fullPath),
				zap.Error(err),
			)
			return fmt.Errorf("cannot set file permissions: %w", err)
		}
	}

	if ownerID != nil || groupID != nil {
		uid := -1
		gid := -1

		if ownerID != nil {
			uid = int(*ownerID)
		}
		if groupID != nil {
			gid = int(*groupID)
		}

		if err := os.Chown(fullPath, uid, gid); err != nil {
			s.logger.Error("cannot change ownership of uploaded file",
				zap.String("operation", "upload_file"),
				zap.String("stack", stackName),
				zap.String("path", path),
				zap.String("full_path", fullPath),
				zap.Int("uid", uid),
				zap.Int("gid", gid),
				zap.Error(err),
			)
			return fmt.Errorf("cannot change ownership: %w", err)
		}
	}

	s.logger.Info("file uploaded successfully",
		zap.String("operation", "upload_file"),
		zap.String("stack", stackName),
		zap.String("path", path),
		zap.String("full_path", fullPath),
		zap.Int64("size", size),
	)

	return nil
}
func (s *Service) Chmod(stackName string, req ChmodRequest) error {
	fullPath, err := s.validateStackPath(stackName, req.Path)
	if err != nil {
		return err
	}

	stat, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		s.logger.Error("path not found for chmod",
			zap.String("operation", "chmod"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
		)
		return fmt.Errorf("path not found: %s", req.Path)
	}
	if err != nil {
		s.logger.Error("cannot stat path for chmod",
			zap.String("operation", "chmod"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot stat path: %w", err)
	}

	mode, err := parseFileMode(req.Mode)
	if err != nil {
		s.logger.Error("invalid file mode for chmod",
			zap.String("operation", "chmod"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("mode", req.Mode),
			zap.Error(err),
		)
		return fmt.Errorf("invalid file mode '%s': %w", req.Mode, err)
	}

	if req.Recursive && stat.IsDir() {
		err := s.chmodRecursive(fullPath, mode)
		if err != nil {
			return err
		}
		s.logger.Info("permissions changed recursively",
			zap.String("operation", "chmod"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
			zap.String("mode", mode.String()),
			zap.Bool("recursive", true),
		)
		return nil
	}

	if err := os.Chmod(fullPath, mode); err != nil {
		s.logger.Error("cannot change permissions",
			zap.String("operation", "chmod"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
			zap.String("mode", mode.String()),
			zap.Error(err),
		)
		return fmt.Errorf("cannot change permissions: %w", err)
	}

	s.logger.Info("permissions changed",
		zap.String("operation", "chmod"),
		zap.String("stack", stackName),
		zap.String("path", req.Path),
		zap.String("full_path", fullPath),
		zap.String("mode", mode.String()),
		zap.Bool("recursive", false),
	)

	return nil
}

func (s *Service) chmodRecursive(path string, mode os.FileMode) error {
	return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if chmodErr := os.Chmod(walkPath, mode); chmodErr != nil {
			return fmt.Errorf("cannot change permissions for %s: %w", walkPath, chmodErr)
		}

		return nil
	})
}

func (s *Service) Chown(stackName string, req ChownRequest) error {
	fullPath, err := s.validateStackPath(stackName, req.Path)
	if err != nil {
		return err
	}

	stat, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		s.logger.Error("path not found for chown",
			zap.String("operation", "chown"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
		)
		return fmt.Errorf("path not found: %s", req.Path)
	}
	if err != nil {
		s.logger.Error("cannot stat path for chown",
			zap.String("operation", "chown"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
			zap.Error(err),
		)
		return fmt.Errorf("cannot stat path: %w", err)
	}

	var uid, gid int = -1, -1

	if req.OwnerID != nil {
		uid = int(*req.OwnerID)
	}

	if req.GroupID != nil {
		gid = int(*req.GroupID)
	}

	if uid == -1 && gid == -1 {
		s.logger.Error("no owner or group specified for chown",
			zap.String("operation", "chown"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
		)
		return fmt.Errorf("either owner_id or group_id must be specified")
	}

	if req.Recursive && stat.IsDir() {
		err := s.chownRecursive(fullPath, uid, gid)
		if err != nil {
			return err
		}
		s.logger.Info("ownership changed recursively",
			zap.String("operation", "chown"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
			zap.Int("uid", uid),
			zap.Int("gid", gid),
			zap.Bool("recursive", true),
		)
		return nil
	}

	if err := os.Chown(fullPath, uid, gid); err != nil {
		s.logger.Error("cannot change ownership",
			zap.String("operation", "chown"),
			zap.String("stack", stackName),
			zap.String("path", req.Path),
			zap.String("full_path", fullPath),
			zap.Int("uid", uid),
			zap.Int("gid", gid),
			zap.Error(err),
		)
		return fmt.Errorf("cannot change ownership: %w", err)
	}

	s.logger.Info("ownership changed",
		zap.String("operation", "chown"),
		zap.String("stack", stackName),
		zap.String("path", req.Path),
		zap.String("full_path", fullPath),
		zap.Int("uid", uid),
		zap.Int("gid", gid),
		zap.Bool("recursive", false),
	)

	return nil
}

func (s *Service) chownRecursive(path string, uid, gid int) error {
	return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if chownErr := os.Chown(walkPath, uid, gid); chownErr != nil {
			return fmt.Errorf("cannot change ownership for %s: %w", walkPath, chownErr)
		}

		return nil
	})
}

func parseFileMode(modeStr string) (os.FileMode, error) {
	if len(modeStr) == 3 || len(modeStr) == 4 {
		mode, err := strconv.ParseUint(modeStr, 8, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid octal mode: %w", err)
		}
		return os.FileMode(mode), nil
	}

	return 0, fmt.Errorf("mode must be 3 or 4 digit octal number (e.g., 755, 0644)")
}

func getUserName(uid uint32) (string, error) {
	u, err := user.LookupId(fmt.Sprintf("%d", uid))
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func getGroupName(gid uint32) (string, error) {
	g, err := user.LookupGroupId(fmt.Sprintf("%d", gid))
	if err != nil {
		return "", err
	}
	return g.Name, nil
}
