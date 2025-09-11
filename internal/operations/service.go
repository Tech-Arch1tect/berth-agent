package operations

import (
	"berth-agent/internal/archive"
	"berth-agent/internal/validation"
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	stackLocation  string
	accessToken    string
	operations     map[string]*Operation
	mutex          sync.RWMutex
	archiveService *archive.Service
}

func NewService(stackLocation, accessToken string) *Service {
	return &Service{
		stackLocation:  stackLocation,
		accessToken:    accessToken,
		operations:     make(map[string]*Operation),
		archiveService: archive.NewService(),
	}
}

func (s *Service) StartOperation(ctx context.Context, stackName string, req OperationRequest) (string, error) {

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, stackName)
	if err != nil {
		return "", fmt.Errorf("invalid stack path: %w", err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return "", fmt.Errorf("stack '%s' not found", stackName)
	}

	// Block stack-wide operations on berth-agent stack to prevent sidecar updating itself
	if stackName == "berth-agent" && len(req.Services) == 0 {
		return "", fmt.Errorf("stack-wide operations are not supported for berth-agent stack - please target specific services only")
	}

	operationID := uuid.New().String()
	isSelfOp := s.isSelfOperation(stackName, req)

	operation := &Operation{
		ID:        operationID,
		StackName: stackName,
		Request:   req,
		StartTime: time.Now(),
		Status:    "running",
		IsSelfOp:  isSelfOp,
	}

	s.mutex.Lock()
	s.operations[operationID] = operation
	s.mutex.Unlock()

	return operationID, nil
}

func (s *Service) isSelfOperation(stackName string, req OperationRequest) bool {
	if stackName != "berth-agent" {
		return false
	}

	// Only "up" and "restart" operations need sidecar handling
	if req.Command != "up" && req.Command != "restart" {
		return false
	}

	// Only consider it a self-operation if targeting the berth-agent service specifically
	if len(req.Services) == 1 && req.Services[0] == "berth-agent" {
		return true
	}

	return false
}

func (s *Service) GetOperation(operationID string) (*Operation, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	op, exists := s.operations[operationID]
	return op, exists
}

func (s *Service) StreamOperation(ctx context.Context, operationID string, writer io.Writer) error {
	operation, exists := s.GetOperation(operationID)
	if !exists {
		return fmt.Errorf("operation not found")
	}

	if operation.IsSelfOp {
		return s.handleSelfOperation(ctx, operation, writer)
	}

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, operation.StackName)
	if err != nil {
		return fmt.Errorf("invalid stack path: %w", err)
	}

	// Handle archive operations differently from Docker commands
	if operation.Request.Command == "create-archive" || operation.Request.Command == "extract-archive" {
		return s.handleArchiveOperation(ctx, operation, stackPath, writer)
	}

	cmd := s.buildCommand(operation.Request, stackPath)
	cmd.Dir = stackPath

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		s.sendMessage(writer, StreamTypeError, fmt.Sprintf("Failed to start command: %v", err))
		return err
	}

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		s.streamOutputWithContext(ctx, stdout, writer, StreamTypeStdout)
	}()

	go func() {
		defer wg.Done()
		s.streamOutputWithContext(ctx, stderr, writer, StreamTypeStderr)
	}()

	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode := exitError.ExitCode()
			s.updateOperationStatus(operationID, "completed", &exitCode)
			s.sendCompleteMessage(writer, false, exitCode)
		} else {
			s.updateOperationStatus(operationID, "failed", nil)
			s.sendMessage(writer, StreamTypeError, fmt.Sprintf("Command execution error: %v", err))
		}
	} else {
		exitCode := 0
		s.updateOperationStatus(operationID, "completed", &exitCode)
		s.sendCompleteMessage(writer, true, exitCode)
	}

	time.Sleep(2 * time.Second)

	return nil
}

func (s *Service) buildCommand(req OperationRequest, stackPath string) *exec.Cmd {

	args := []string{"compose", req.Command}

	filteredOptions := make([]string, 0, len(req.Options))
	for _, option := range req.Options {
		if option != "-d" && option != "--detach" {
			filteredOptions = append(filteredOptions, option)
		}
	}

	if req.Command == "up" {
		filteredOptions = append(filteredOptions, "-d")
	}

	args = append(args, filteredOptions...)
	args = append(args, req.Services...)

	cmd := exec.Command("docker", args...)

	cmd.Dir = stackPath

	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/tmp",
	}

	return cmd
}

func (s *Service) handleArchiveOperation(ctx context.Context, operation *Operation, stackPath string, writer io.Writer) error {
	progressWriter := archive.NewOperationsProgressWriter(writer)

	var err error
	switch operation.Request.Command {
	case "create-archive":
		err = s.handleCreateArchive(ctx, operation, stackPath, progressWriter)
	case "extract-archive":
		err = s.handleExtractArchive(ctx, operation, stackPath, progressWriter)
	default:
		s.updateOperationStatus(operation.ID, "failed", nil)
		s.sendMessage(writer, StreamTypeError, fmt.Sprintf("Unknown archive command: %s", operation.Request.Command))
		return fmt.Errorf("unknown archive command: %s", operation.Request.Command)
	}

	if err != nil {
		s.updateOperationStatus(operation.ID, "failed", nil)
		progressWriter.WriteError(fmt.Sprintf("Archive operation failed: %v", err))
		return err
	}

	exitCode := 0
	s.updateOperationStatus(operation.ID, "completed", &exitCode)
	progressWriter.WriteStdout("Archive operation completed successfully")
	s.sendCompleteMessage(writer, true, exitCode)

	return nil
}

func (s *Service) handleCreateArchive(ctx context.Context, operation *Operation, stackPath string, writer archive.ProgressWriter) error {
	opts := archive.CreateOptions{
		Format:      "zip",
		Compression: "gzip",
	}

	options := operation.Request.Options
	for i, opt := range options {
		switch opt {
		case "--format":
			if i+1 < len(options) {
				opts.Format = options[i+1]
			}
		case "--output":
			if i+1 < len(options) {
				opts.OutputPath = options[i+1]
			}
		case "--include":
			if i+1 < len(options) {
				opts.IncludePaths = append(opts.IncludePaths, options[i+1])
			}
		case "--exclude":
			if i+1 < len(options) {
				opts.ExcludePatterns = append(opts.ExcludePatterns, options[i+1])
			}
		case "--compression":
			if i+1 < len(options) {
				opts.Compression = options[i+1]
			}
		}
	}

	return s.archiveService.CreateArchive(ctx, stackPath, opts, writer)
}

func (s *Service) handleExtractArchive(ctx context.Context, operation *Operation, stackPath string, writer archive.ProgressWriter) error {
	opts := archive.ExtractOptions{
		DestinationPath: ".",
	}

	options := operation.Request.Options
	for i, opt := range options {
		switch opt {
		case "--archive":
			if i+1 < len(options) {
				opts.ArchivePath = options[i+1]
			}
		case "--destination":
			if i+1 < len(options) {
				opts.DestinationPath = options[i+1]
			}
		case "--overwrite":
			opts.Overwrite = true
		case "--create-dirs":
			opts.CreateDirs = true
		}
	}

	return s.archiveService.ExtractArchive(ctx, stackPath, opts, writer)
}

func (s *Service) streamOutputWithContext(ctx context.Context, reader io.Reader, writer io.Writer, streamType StreamMessageType) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			line := scanner.Text()
			s.sendMessage(writer, streamType, line)
		}
	}
}

func (s *Service) sendMessage(writer io.Writer, msgType StreamMessageType, data string) {
	message := StreamMessage{
		Type:      string(msgType),
		Data:      data,
		Timestamp: time.Now(),
	}

	output := fmt.Sprintf("data: {\"type\":\"%s\",\"data\":\"%s\",\"timestamp\":\"%s\"}\n\n",
		message.Type,
		strings.ReplaceAll(data, "\"", "\\\""),
		message.Timestamp.Format(time.RFC3339))

	_, err := writer.Write([]byte(output))
	if err != nil {
		return
	}

	if flusher, ok := writer.(interface{ Flush() }); ok {
		defer func() {
			_ = recover()
		}()
		flusher.Flush()
	}
}

func (s *Service) sendCompleteMessage(writer io.Writer, success bool, exitCode int) {
	message := StreamMessage{
		Type:      string(StreamTypeComplete),
		Data:      "",
		Timestamp: time.Now(),
	}

	messageJSON, err := json.Marshal(map[string]any{
		"type":      message.Type,
		"success":   success,
		"exitCode":  exitCode,
		"timestamp": message.Timestamp,
	})
	if err != nil {
		return
	}

	output := fmt.Sprintf("data: %s\n\n", messageJSON)
	_, err = writer.Write([]byte(output))
	if err != nil {
		return
	}

	if flusher, ok := writer.(interface{ Flush() }); ok {
		defer func() {
			_ = recover()
		}()
		flusher.Flush()
	}
}

func (s *Service) handleSelfOperation(ctx context.Context, operation *Operation, writer io.Writer) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, operation.StackName)
	if err != nil {
		s.updateOperationStatus(operation.ID, "failed", nil)
		return fmt.Errorf("invalid stack path: %w", err)
	}

	payload := map[string]any{
		"command":    operation.Request.Command,
		"options":    operation.Request.Options,
		"services":   operation.Request.Services,
		"stack_path": stackPath,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		s.updateOperationStatus(operation.ID, "failed", nil)
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://berth-updater:8081/operation", bytes.NewBuffer(jsonData))
	if err != nil {
		s.updateOperationStatus(operation.ID, "failed", nil)
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.accessToken)

	resp, err := client.Do(req)
	if err != nil {
		s.updateOperationStatus(operation.ID, "failed", nil)
		return fmt.Errorf("failed to connect to sidecar: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.updateOperationStatus(operation.ID, "failed", nil)
		return fmt.Errorf("sidecar returned status: %d", resp.StatusCode)
	}

	var sidecarResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&sidecarResp); err != nil {
		s.updateOperationStatus(operation.ID, "failed", nil)
		return fmt.Errorf("failed to parse sidecar response: %w", err)
	}

	s.sendMessage(writer, StreamTypeStdout, "Detected self-operation, forwarding to sidecar updater...")
	if message, ok := sidecarResp["message"]; ok {
		s.sendMessage(writer, StreamTypeStdout, message)
	}
	s.sendMessage(writer, StreamTypeStdout, fmt.Sprintf("Sidecar will handle %s operation independently", operation.Request.Command))
	s.sendMessage(writer, StreamTypeStdout, "Agent update will continue in background after this connection closes")

	exitCode := 0
	s.updateOperationStatus(operation.ID, "completed", &exitCode)
	s.sendCompleteMessage(writer, true, exitCode)

	return nil
}

func (s *Service) updateOperationStatus(operationID, status string, exitCode *int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if op, exists := s.operations[operationID]; exists {
		op.Status = status
		op.ExitCode = exitCode
	}
}
