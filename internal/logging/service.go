package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Service struct {
	logger          *zap.Logger
	fileWriter      *os.File
	writeMutex      sync.Mutex
	enabled         bool
	logDir          string
	logBaseName     string
	logExtension    string
	currentDate     string
	currentFilePath string
}

func NewService(enabled bool, logFilePath string) (*Service, error) {
	if !enabled {
		return &Service{
			enabled: false,
		}, nil
	}

	if logFilePath == "" {
		logFilePath = "/var/log/berth-agent/requests.jsonl"
	}

	logDir := filepath.Dir(logFilePath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
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
	}

	if err := service.ensureCurrentLogFile(); err != nil {
		logger.Sync()
		return nil, err
	}

	return service, nil
}

func (s *Service) LogRequest(entry *RequestLogEntry) {
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

	jsonData, err := json.Marshal(entry)
	if err != nil {
		s.logger.Error("failed to marshal log entry", zap.Error(err))
		return
	}

	if _, err := s.fileWriter.Write(append(jsonData, '\n')); err != nil {
		s.logger.Error("failed to write log entry", zap.Error(err))
		return
	}
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

func (s *Service) RotateIfNeeded() error {
	if !s.enabled || s.fileWriter == nil {
		return nil
	}

	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()

	if err := s.ensureCurrentLogFileLocked(); err != nil {
		return err
	}

	if s.fileWriter == nil {
		return nil
	}

	info, err := s.fileWriter.Stat()
	if err != nil {
		return err
	}

	const maxSize = 100 * 1024 * 1024
	if info.Size() < maxSize {
		return nil
	}

	if err := s.fileWriter.Close(); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102-150405")
	rotatedPath := fmt.Sprintf("%s.%s", s.currentFilePath, timestamp)
	if err := os.Rename(s.currentFilePath, rotatedPath); err != nil {
		return err
	}

	newFile, err := os.OpenFile(s.currentFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	s.fileWriter = newFile
	return nil
}

func (s *Service) ensureCurrentLogFile() error {
	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()
	return s.ensureCurrentLogFileLocked()
}

func (s *Service) ensureCurrentLogFileLocked() error {
	if !s.enabled {
		return nil
	}

	currentDate := time.Now().Format("2006-01-02")
	if s.fileWriter != nil && s.currentDate == currentDate {
		return nil
	}

	if s.fileWriter != nil {
		if err := s.fileWriter.Close(); err != nil {
			return fmt.Errorf("failed to close audit log file: %w", err)
		}
	}

	filename := filepath.Join(s.logDir, fmt.Sprintf("%s-%s%s", s.logBaseName, currentDate, s.logExtension))
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open audit log file: %w", err)
	}

	s.fileWriter = file
	s.currentDate = currentDate
	s.currentFilePath = filename

	if s.logger != nil {
		s.logger.Debug("opened audit log file", zap.String("path", filename))
	}

	return nil
}
