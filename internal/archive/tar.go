package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type TarHandler struct{}

func NewTarHandler() *TarHandler {
	return &TarHandler{}
}

func (h *TarHandler) Create(ctx context.Context, basePath string, opts CreateOptions, writer ProgressWriter) error {
	tarFile, err := os.Create(opts.OutputPath)
	if err != nil {
		return fmt.Errorf("failed to create tar file: %w", err)
	}
	defer tarFile.Close()

	var tarWriter *tar.Writer
	compress := opts.Compression == "gzip" || strings.HasSuffix(opts.Format, ".gz")

	if compress {
		gzWriter := gzip.NewWriter(tarFile)
		defer gzWriter.Close()
		tarWriter = tar.NewWriter(gzWriter)
	} else {
		tarWriter = tar.NewWriter(tarFile)
	}
	defer tarWriter.Close()

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

			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}

			header.Name = relPath

			if err := tarWriter.WriteHeader(header); err != nil {
				return err
			}

			if !info.IsDir() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer file.Close()

				_, err = io.Copy(tarWriter, file)
				if err != nil {
					return err
				}

				fileCount++
				if fileCount%100 == 0 {
					writer.WriteStdout(fmt.Sprintf("Added %d files to archive...", fileCount))
				}
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

func (h *TarHandler) Extract(ctx context.Context, opts ExtractOptions, writer ProgressWriter) error {
	file, err := os.Open(opts.ArchivePath)
	if err != nil {
		return fmt.Errorf("failed to open tar file: %w", err)
	}
	defer file.Close()

	var tarReader *tar.Reader
	if strings.HasSuffix(strings.ToLower(opts.ArchivePath), ".tar.gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		tarReader = tar.NewReader(gzReader)
	} else {
		tarReader = tar.NewReader(file)
	}

	if opts.CreateDirs {
		if err := os.MkdirAll(opts.DestinationPath, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}
	}

	fileCount := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		path, err := ValidateExtractPath(opts.DestinationPath, header.Name)
		if err != nil {
			writer.WriteError(fmt.Sprintf("Skipping file outside destination: %s", header.Name))
			continue
		}

		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(path, header.FileInfo().Mode()); err != nil {
				writer.WriteError(fmt.Sprintf("Failed to create directory %s: %v", path, err))
			}
			continue
		}

		if _, err := os.Stat(path); err == nil && !opts.Overwrite {
			writer.WriteStdout(fmt.Sprintf("Skipping existing file: %s", header.Name))
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			writer.WriteError(fmt.Sprintf("Failed to create parent directory for %s: %v", path, err))
			continue
		}

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, header.FileInfo().Mode())
		if err != nil {
			writer.WriteError(fmt.Sprintf("Failed to create file %s: %v", path, err))
			continue
		}

		_, err = io.Copy(outFile, tarReader)
		outFile.Close()

		if err != nil {
			writer.WriteError(fmt.Sprintf("Failed to extract file %s: %v", header.Name, err))
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
