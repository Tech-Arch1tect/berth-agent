package backup

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	helperRepoPath   = "/berth-backup/repo"
	helperSourceRoot = "/berth-backup/source"

	resticExitRepoDoesNotExist = 10
	resticExitRepoLocked       = 11
	resticExitWrongPassword    = 12
)

func resticEnv(password string, s3Repo *S3Repository) []string {
	env := []string{"RESTIC_PASSWORD=" + password}
	if s3Repo != nil {
		env = append(env,
			"RESTIC_REPOSITORY="+s3Repo.URL,
			"AWS_ACCESS_KEY_ID="+s3Repo.AccessKeyID,
			"AWS_SECRET_ACCESS_KEY="+s3Repo.SecretKey,
		)
		if s3Repo.Region != "" {
			env = append(env, "AWS_DEFAULT_REGION="+s3Repo.Region)
		}
		return env
	}
	return append(env, "RESTIC_REPOSITORY="+helperRepoPath)
}

var resticPatternEscaper = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`)

func escapeResticPattern(path string) string {
	return resticPatternEscaper.Replace(path)
}

func componentSourceMountPath(c Component) string {
	return helperSourceRoot + "/" + componentMountName(c)
}

func backupArgs(c Component, stackName, runID, label string) []string {
	sourcePath := componentSourceMountPath(c)
	args := []string{
		"backup",
		sourcePath,
		"--json",
		"--host", stackName,
		"--tag", "run:" + runID,
		"--tag", "component:" + c.ID,
	}
	if label != "" {
		args = append(args, "--tag", "label:"+label)
	}
	for _, exclude := range c.Excludes {
		args = append(args, "--exclude", sourcePath+"/"+escapeResticPattern(exclude))
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
	componentID    string
	writer         ProgressWriter
	now            func() time.Time
	lastProgress   time.Time
	summary        *backupSummary
	errors         []string
	captureFailure func(string)
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
	if p.captureFailure != nil {
		p.captureFailure(line)
	}

	if !strings.HasPrefix(line, "{") {
		if p.captureFailure == nil {
			p.writer.WriteStdout(line)
		}
		return
	}

	var event resticEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		if p.captureFailure == nil {
			p.writer.WriteStdout(line)
		}
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
		if p.captureFailure == nil {
			message := fmt.Sprintf("%s: error during %s on %s: %v", p.componentID, event.During, event.Item, event.Error)
			p.errors = append(p.errors, message)
			p.writer.WriteStderr(message)
		}
	case "verbose_status":
	default:
		if p.captureFailure == nil {
			p.writer.WriteStdout(line)
		}
	}
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
