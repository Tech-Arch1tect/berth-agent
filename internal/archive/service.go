package archive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Service struct {
	handlers map[string]ArchiveHandler
}

func NewService() *Service {
	return &Service{
		handlers: map[string]ArchiveHandler{
			"zip":    NewZipHandler(),
			"tar":    NewTarHandler(),
			"tar.gz": NewTarHandler(),
		},
	}
}

func (s *Service) CreateArchive(ctx context.Context, basePath string, opts CreateOptions, writer ProgressWriter) error {
	handler, exists := s.handlers[opts.Format]
	if !exists {
		return fmt.Errorf("unsupported format: %s", opts.Format)
	}

	if len(opts.IncludePaths) == 0 {
		opts.IncludePaths = []string{"."}
	}

	if opts.OutputPath == "" {
		return fmt.Errorf("output path is required")
	}

	outputPath := filepath.Join(basePath, opts.OutputPath)
	if err := EnsureWithinStackPath(outputPath, basePath); err != nil {
		return err
	}

	opts.OutputPath = outputPath
	writer.WriteStdout(fmt.Sprintf("Creating %s archive: %s", opts.Format, filepath.Base(outputPath)))

	return handler.Create(ctx, basePath, opts, writer)
}

func (s *Service) ExtractArchive(ctx context.Context, basePath string, opts ExtractOptions, writer ProgressWriter) error {
	if opts.ArchivePath == "" {
		return fmt.Errorf("archive path is required")
	}

	if opts.DestinationPath == "" {
		opts.DestinationPath = "."
	}

	archivePath := filepath.Join(basePath, opts.ArchivePath)
	destinationPath := filepath.Join(basePath, opts.DestinationPath)

	if err := EnsureWithinStackPath(archivePath, basePath); err != nil {
		return err
	}
	if err := EnsureWithinStackPath(destinationPath, basePath); err != nil {
		return err
	}

	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		return fmt.Errorf("archive not found: %s", filepath.Base(archivePath))
	}

	format := s.detectFormat(archivePath)
	handler, exists := s.handlers[format]
	if !exists {
		return fmt.Errorf("unsupported archive format: %s", format)
	}

	opts.ArchivePath = archivePath
	opts.DestinationPath = destinationPath

	writer.WriteStdout(fmt.Sprintf("Extracting archive: %s", filepath.Base(archivePath)))

	return handler.Extract(ctx, opts, writer)
}

func (s *Service) detectFormat(archivePath string) string {
	ext := strings.ToLower(filepath.Ext(archivePath))

	if ext == ".zip" {
		return "zip"
	}

	if ext == ".tar" || strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz") {
		if strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz") {
			return "tar.gz"
		}
		return "tar"
	}

	return ext
}
