package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Service struct {
	logger        *zap.Logger
	fileWriter    *os.File
	writeMutex    sync.Mutex
	enabled       bool
	logDir        string
	logBaseName   string
	logExtension  string
	currentDate   string
	currentSeqNum int
	maxSizeBytes  int64
}

type AuditEvent struct {
	Timestamp     time.Time      `json:"timestamp"`
	EventType     string         `json:"event_type"`
	EventCategory string         `json:"event_category"`
	Severity      string         `json:"severity"`
	Success       bool           `json:"success"`
	ClientIP      string         `json:"client_ip,omitempty"`
	StackName     string         `json:"stack_name,omitempty"`
	TargetPath    string         `json:"target_path,omitempty"`
	OperationID   string         `json:"operation_id,omitempty"`
	Command       string         `json:"command,omitempty"`
	FailureReason string         `json:"failure_reason,omitempty"`
	DurationMs    int64          `json:"duration_ms,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

func NewService(enabled bool, logFilePath string, maxSizeBytes int64) (*Service, error) {
	if !enabled {
		return &Service{
			enabled: false,
		}, nil
	}

	if logFilePath == "" {
		logFilePath = "/var/log/berth-agent/audit.jsonl"
	}

	logDir := filepath.Dir(logFilePath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audit log directory: %w", err)
	}

	config := zap.NewProductionConfig()
	config.OutputPaths = []string{"stdout"}
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	baseName := filepath.Base(logFilePath)
	ext := filepath.Ext(baseName)
	if ext == "" {
		ext = ".jsonl"
	}
	trimmedBase := strings.TrimSuffix(baseName, ext)
	if trimmedBase == "" {
		trimmedBase = "audit"
	}

	service := &Service{
		logger:       logger,
		enabled:      true,
		logDir:       logDir,
		logBaseName:  trimmedBase,
		logExtension: ext,
		maxSizeBytes: maxSizeBytes,
		currentDate:  time.Now().Format("2006-01-02"),
	}

	if err := service.scanExistingSequenceNumber(); err != nil {
		logger.Warn("failed to scan existing sequence numbers", zap.Error(err))
	}

	if err := service.openCurrentFile(); err != nil {
		logger.Sync()
		return nil, err
	}

	logger.Info("audit log service initialized",
		zap.String("log_dir", logDir),
		zap.String("base_name", trimmedBase),
		zap.Int64("max_size_bytes", maxSizeBytes),
		zap.Int("current_seq_num", service.currentSeqNum),
	)

	return service, nil
}

func (s *Service) currentFilePath() string {
	return filepath.Join(s.logDir, fmt.Sprintf("%s-current%s", s.logBaseName, s.logExtension))
}

func (s *Service) rotatedFilePath(date string, seqNum int) string {
	if seqNum == 0 {
		return filepath.Join(s.logDir, fmt.Sprintf("%s-%s%s", s.logBaseName, date, s.logExtension))
	}
	return filepath.Join(s.logDir, fmt.Sprintf("%s-%s-%d%s", s.logBaseName, date, seqNum, s.logExtension))
}

func (s *Service) scanExistingSequenceNumber() error {
	escapedExt := regexp.QuoteMeta(s.logExtension)
	pattern := regexp.MustCompile(fmt.Sprintf(`^%s-%s-(\d+)%s$`, regexp.QuoteMeta(s.logBaseName), regexp.QuoteMeta(s.currentDate), escapedExt))

	entries, err := os.ReadDir(s.logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	maxSeq := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := pattern.FindStringSubmatch(entry.Name())
		if len(matches) == 2 {
			if seq, err := strconv.Atoi(matches[1]); err == nil && seq > maxSeq {
				maxSeq = seq
			}
		}
	}

	s.currentSeqNum = maxSeq
	return nil
}

func (s *Service) openCurrentFile() error {
	path := s.currentFilePath()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open audit log file %s: %w", path, err)
	}
	s.fileWriter = file

	if s.logger != nil {
		s.logger.Debug("opened current audit log file", zap.String("path", path))
	}

	return nil
}

func (s *Service) rotateMidnight(previousDate string) error {
	if s.fileWriter != nil {
		if err := s.fileWriter.Close(); err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to close current audit log file during midnight rotation", zap.Error(err))
			}
		}
		s.fileWriter = nil
	}

	currentPath := s.currentFilePath()

	info, err := os.Stat(currentPath)
	if err == nil && info.Size() > 0 {
		var rotatedPath string
		if s.currentSeqNum > 0 {
			rotatedPath = s.rotatedFilePath(previousDate, s.currentSeqNum+1)
		} else {
			rotatedPath = s.rotatedFilePath(previousDate, 0)
		}

		if err := os.Rename(currentPath, rotatedPath); err != nil {
			if s.logger != nil {
				s.logger.Error("failed to rotate audit log file at midnight",
					zap.String("from", currentPath),
					zap.String("to", rotatedPath),
					zap.Error(err),
				)
			}
			return fmt.Errorf("failed to rotate audit log file: %w", err)
		}

		if s.logger != nil {
			s.logger.Info("rotated audit log file at midnight", zap.String("rotated_to", rotatedPath))
		}
	}

	s.currentSeqNum = 0
	return s.openCurrentFile()
}

func (s *Service) rotateSize() error {
	if s.fileWriter != nil {
		if err := s.fileWriter.Close(); err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to close current audit log file during size rotation", zap.Error(err))
			}
		}
		s.fileWriter = nil
	}

	currentPath := s.currentFilePath()
	s.currentSeqNum++

	rotatedPath := s.rotatedFilePath(s.currentDate, s.currentSeqNum)
	if err := os.Rename(currentPath, rotatedPath); err != nil {
		if s.logger != nil {
			s.logger.Error("failed to rotate audit log file by size",
				zap.String("from", currentPath),
				zap.String("to", rotatedPath),
				zap.Error(err),
			)
		}
		return fmt.Errorf("failed to rotate audit log file: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("rotated audit log file by size",
			zap.String("rotated_to", rotatedPath),
			zap.Int("seq_num", s.currentSeqNum),
		)
	}

	return s.openCurrentFile()
}

func (s *Service) ensureCurrentLogFileLocked() error {
	if !s.enabled {
		return nil
	}

	today := time.Now().Format("2006-01-02")

	if s.currentDate != "" && s.currentDate != today {
		previousDate := s.currentDate
		s.currentDate = today
		if err := s.rotateMidnight(previousDate); err != nil {
			return err
		}
		return nil
	}

	if s.fileWriter == nil {
		s.currentDate = today
		return s.openCurrentFile()
	}

	return nil
}

func (s *Service) checkSizeRotation() {
	if s.maxSizeBytes <= 0 || s.fileWriter == nil {
		return
	}

	info, err := s.fileWriter.Stat()
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to stat current audit log file for size check", zap.Error(err))
		}
		return
	}

	if info.Size() >= s.maxSizeBytes {
		if err := s.rotateSize(); err != nil {
			if s.logger != nil {
				s.logger.Error("failed to rotate audit log file by size", zap.Error(err))
			}
		}
	}
}

func (s *Service) Log(event AuditEvent) {
	if !s.enabled {
		return
	}

	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()

	if err := s.ensureCurrentLogFileLocked(); err != nil {
		if s.logger != nil {
			s.logger.Error("failed to ensure audit log file", zap.Error(err))
		}
		return
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	event.EventCategory = GetEventCategory(event.EventType)
	event.Severity = GetEventSeverity(event.EventType)

	jsonData, err := json.Marshal(event)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("failed to marshal audit event", zap.Error(err))
		}
		return
	}

	if _, err := s.fileWriter.Write(append(jsonData, '\n')); err != nil {
		if s.logger != nil {
			s.logger.Error("failed to write audit event", zap.Error(err))
		}
		return
	}

	s.checkSizeRotation()
}

func (s *Service) LogFileEvent(eventType string, clientIP string, stackName string, targetPath string, success bool, failureReason string, metadata map[string]any) {
	s.Log(AuditEvent{
		EventType:     eventType,
		ClientIP:      clientIP,
		StackName:     stackName,
		TargetPath:    targetPath,
		Success:       success,
		FailureReason: failureReason,
		Metadata:      metadata,
	})
}

func (s *Service) LogStackEvent(eventType string, clientIP string, stackName string, success bool, failureReason string, metadata map[string]any) {
	s.Log(AuditEvent{
		EventType:     eventType,
		ClientIP:      clientIP,
		StackName:     stackName,
		Success:       success,
		FailureReason: failureReason,
		Metadata:      metadata,
	})
}

func (s *Service) LogOperationEvent(eventType string, clientIP string, stackName string, operationID string, command string, success bool, failureReason string, durationMs int64, metadata map[string]any) {
	s.Log(AuditEvent{
		EventType:     eventType,
		ClientIP:      clientIP,
		StackName:     stackName,
		OperationID:   operationID,
		Command:       command,
		Success:       success,
		FailureReason: failureReason,
		DurationMs:    durationMs,
		Metadata:      metadata,
	})
}

func (s *Service) LogMaintenanceEvent(eventType string, clientIP string, success bool, failureReason string, metadata map[string]any) {
	s.Log(AuditEvent{
		EventType:     eventType,
		ClientIP:      clientIP,
		Success:       success,
		FailureReason: failureReason,
		Metadata:      metadata,
	})
}

func (s *Service) LogVulnscanEvent(eventType string, clientIP string, stackName string, success bool, failureReason string, metadata map[string]any) {
	s.Log(AuditEvent{
		EventType:     eventType,
		ClientIP:      clientIP,
		StackName:     stackName,
		Success:       success,
		FailureReason: failureReason,
		Metadata:      metadata,
	})
}

func (s *Service) LogTerminalEvent(eventType string, clientIP string, stackName string, containerName string, metadata map[string]any) {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["container"] = containerName

	s.Log(AuditEvent{
		EventType: eventType,
		ClientIP:  clientIP,
		StackName: stackName,
		Success:   true,
		Metadata:  metadata,
	})
}

func (s *Service) LogAuthEvent(eventType string, clientIP string, success bool, failureReason string) {
	s.Log(AuditEvent{
		EventType:     eventType,
		ClientIP:      clientIP,
		Success:       success,
		FailureReason: failureReason,
	})
}

func (s *Service) Close() error {
	if !s.enabled {
		return nil
	}

	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()

	if s.logger != nil {
		s.logger.Sync()
	}
	if s.fileWriter != nil {
		return s.fileWriter.Close()
	}
	return nil
}

func (s *Service) IsEnabled() bool {
	return s.enabled
}
