package archive

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type ZipHandler struct{}

func NewZipHandler() *ZipHandler {
	return &ZipHandler{}
}

func (h *ZipHandler) Create(ctx context.Context, basePath string, opts CreateOptions, writer ProgressWriter) error {
	zipFile, err := os.Create(opts.OutputPath)
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	fileCount := 0

	for _, includePath := range opts.IncludePaths {
		fullPath := filepath.Join(basePath, includePath)

		err := filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if err != nil {
				writer.WriteError(fmt.Sprintf("Error accessing %s: %v", path, err))
				return nil
			}

			relPath, _ := filepath.Rel(basePath, path)
			for _, pattern := range opts.ExcludePatterns {
				if matched, _ := filepath.Match(pattern, relPath); matched {
					return nil
				}
			}

			if info.IsDir() {
				return nil
			}

			relPath, err = filepath.Rel(basePath, path)
			if err != nil {
				return err
			}

			fileHeader, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			fileHeader.Name = relPath
			fileHeader.Method = zip.Deflate

			w, err := zipWriter.CreateHeader(fileHeader)
			if err != nil {
				return err
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(w, file)
			if err != nil {
				return err
			}

			fileCount++
			if fileCount%100 == 0 {
				writer.WriteStdout(fmt.Sprintf("Added %d files to archive...", fileCount))
			}

			return nil
		})

		if err != nil {
			return err
		}
	}

	writer.WriteStdout(fmt.Sprintf("Archive created with %d files", fileCount))
	return nil
}

func (h *ZipHandler) Extract(ctx context.Context, opts ExtractOptions, writer ProgressWriter) error {
	reader, err := zip.OpenReader(opts.ArchivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer reader.Close()

	if opts.CreateDirs {
		if err := os.MkdirAll(opts.DestinationPath, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}
	}

	fileCount := 0
	for _, file := range reader.File {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		path, err := ValidateExtractPath(opts.DestinationPath, file.Name)
		if err != nil {
			writer.WriteError(fmt.Sprintf("Skipping file outside destination: %s", file.Name))
			continue
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(path, file.FileInfo().Mode()); err != nil {
				writer.WriteError(fmt.Sprintf("Failed to create directory %s: %v", path, err))
			}
			continue
		}

		if _, err := os.Stat(path); err == nil && !opts.Overwrite {
			writer.WriteStdout(fmt.Sprintf("Skipping existing file: %s", file.Name))
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			writer.WriteError(fmt.Sprintf("Failed to create parent directory for %s: %v", path, err))
			continue
		}

		reader, err := file.Open()
		if err != nil {
			writer.WriteError(fmt.Sprintf("Failed to open file %s: %v", file.Name, err))
			continue
		}

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.FileInfo().Mode())
		if err != nil {
			reader.Close()
			writer.WriteError(fmt.Sprintf("Failed to create file %s: %v", path, err))
			continue
		}

		_, err = io.Copy(outFile, reader)
		reader.Close()
		outFile.Close()

		if err != nil {
			writer.WriteError(fmt.Sprintf("Failed to extract file %s: %v", file.Name, err))
			continue
		}

		fileCount++
		if fileCount%100 == 0 {
			writer.WriteStdout(fmt.Sprintf("Extracted %d files...", fileCount))
		}
	}

	writer.WriteStdout(fmt.Sprintf("Extracted %d files", fileCount))
	return nil
}
