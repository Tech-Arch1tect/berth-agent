package archive

import (
	"context"
)

type CreateOptions struct {
	Format          string
	OutputPath      string
	IncludePaths    []string
	ExcludePatterns []string
	Compression     string
}

type ExtractOptions struct {
	ArchivePath     string
	DestinationPath string
	Overwrite       bool
	CreateDirs      bool
}

type ProgressWriter interface {
	WriteMessage(msgType string, data string)
	WriteError(data string)
	WriteStdout(data string)
}

type ArchiveHandler interface {
	Create(ctx context.Context, basePath string, opts CreateOptions, writer ProgressWriter) error
	Extract(ctx context.Context, opts ExtractOptions, writer ProgressWriter) error
}
