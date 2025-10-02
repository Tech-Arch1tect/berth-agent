package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Service struct {
	logger      *zap.Logger
	fileWriter  *os.File
	writeMutex  sync.Mutex
	enabled     bool
	logFilePath string
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

	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	config := zap.NewProductionConfig()
	config.OutputPaths = []string{"stdout"}
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logger, err := config.Build()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	return &Service{
		logger:      logger,
		fileWriter:  file,
		enabled:     true,
		logFilePath: logFilePath,
	}, nil
}

func (s *Service) LogRequest(entry *RequestLogEntry) {
	if !s.enabled {
		return
	}

	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()

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

	info, err := s.fileWriter.Stat()
	if err != nil {
		return err
	}

	const maxSize = 100 * 1024 * 1024
	if info.Size() < maxSize {
		return nil
	}

	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()

	if err := s.fileWriter.Close(); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102-150405")
	rotatedPath := fmt.Sprintf("%s.%s", s.logFilePath, timestamp)
	if err := os.Rename(s.logFilePath, rotatedPath); err != nil {
		return err
	}

	newFile, err := os.OpenFile(s.logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	s.fileWriter = newFile
	return nil
}
