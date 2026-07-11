package backup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	helperRepoPath   = "/berth-backup/repo"
	helperSourceRoot = "/berth-backup/source"

	resticExitRepoDoesNotExist = 10
	resticExitWrongPassword    = 12
)

func resticEnv(password string) []string {
	return []string{
		"RESTIC_REPOSITORY=" + helperRepoPath,
		"RESTIC_PASSWORD=" + password,
	}
}

func componentSourceMountPath(c Component) string {
	return helperSourceRoot + "/" + componentMountName(c)
}

func backupArgs(c Component, stackName, runID string) []string {
	sourcePath := componentSourceMountPath(c)
	args := []string{
		"backup",
		sourcePath,
		"--json",
		"--host", stackName,
		"--tag", "run:" + runID,
		"--tag", "component:" + c.ID,
	}
	for _, exclude := range c.Excludes {
		args = append(args, "--exclude", sourcePath+"/"+exclude)
	}
	return args
}

type resticEvent struct {
	MessageType         string  `json:"message_type"`
	PercentDone         float64 `json:"percent_done"`
	TotalFiles          uint64  `json:"total_files"`
	FilesDone           uint64  `json:"files_done"`
	TotalBytes          uint64  `json:"total_bytes"`
	BytesDone           uint64  `json:"bytes_done"`
	SnapshotID          string  `json:"snapshot_id"`
	FilesNew            uint64  `json:"files_new"`
	FilesChanged        uint64  `json:"files_changed"`
	FilesUnmodified     uint64  `json:"files_unmodified"`
	DataAdded           uint64  `json:"data_added"`
	TotalBytesProcessed uint64  `json:"total_bytes_processed"`
	TotalDuration       float64 `json:"total_duration"`
	Item                string  `json:"item"`
	During              string  `json:"during"`
	Error               any     `json:"error"`
}

type backupSummary struct {
	SnapshotID          string
	FilesNew            uint64
	FilesChanged        uint64
	FilesUnmodified     uint64
	DataAdded           uint64
	TotalBytesProcessed uint64
	TotalDuration       float64
}

type resticOutputParser struct {
	componentID  string
	writer       ProgressWriter
	now          func() time.Time
	lastProgress time.Time
	summary      *backupSummary
	errors       []string
}

func newResticOutputParser(componentID string, writer ProgressWriter) *resticOutputParser {
	return &resticOutputParser{
		componentID: componentID,
		writer:      writer,
		now:         time.Now,
	}
}

func (p *resticOutputParser) handleLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	if !strings.HasPrefix(line, "{") {
		p.writer.WriteStdout(line)
		return
	}

	var event resticEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		p.writer.WriteStdout(line)
		return
	}

	switch event.MessageType {
	case "status":
		if p.now().Sub(p.lastProgress) < 2*time.Second {
			return
		}
		p.lastProgress = p.now()
		p.writer.WriteProgress(fmt.Sprintf("%s: %.0f%% (%d/%d files, %s/%s)",
			p.componentID,
			event.PercentDone*100,
			event.FilesDone, event.TotalFiles,
			formatBytes(event.BytesDone), formatBytes(event.TotalBytes)))
	case "summary":
		p.summary = &backupSummary{
			SnapshotID:          event.SnapshotID,
			FilesNew:            event.FilesNew,
			FilesChanged:        event.FilesChanged,
			FilesUnmodified:     event.FilesUnmodified,
			DataAdded:           event.DataAdded,
			TotalBytesProcessed: event.TotalBytesProcessed,
			TotalDuration:       event.TotalDuration,
		}
	case "error":
		message := fmt.Sprintf("%s: error during %s on %s: %v", p.componentID, event.During, event.Item, event.Error)
		p.errors = append(p.errors, message)
		p.writer.WriteStderr(message)
	case "verbose_status":
	default:
		p.writer.WriteStdout(line)
	}
}

func newLargeScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return scanner
}

func formatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
