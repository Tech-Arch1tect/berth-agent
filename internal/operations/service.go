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
	stackLocation    string
	accessToken      string
	operations       map[string]*Operation
	activeOperations map[string]string
	mutex            sync.RWMutex
	archiveService   *archive.Service
}

func NewService(stackLocation, accessToken string) *Service {
	return &Service{
		stackLocation:    stackLocation,
		accessToken:      accessToken,
		operations:       make(map[string]*Operation),
		activeOperations: make(map[string]string),
		archiveService:   archive.NewService(),
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

	if stackName == "berth-agent" && len(req.Services) == 0 {
		return "", fmt.Errorf("stack-wide operations are not supported for berth-agent stack - please target specific services only")
	}

	s.mutex.Lock()
	if existingOpID, exists := s.activeOperations[stackName]; exists {
		s.mutex.Unlock()
		return "", fmt.Errorf("another operation (%s) is already running on stack '%s'", existingOpID, stackName)
	}

	operationID := uuid.New().String()
	isSelfOp := s.isSelfOperation(stackName, req)

	broadcaster := NewBroadcaster(operationID)

	operation := &Operation{
		ID:          operationID,
		StackName:   stackName,
		Request:     req,
		StartTime:   time.Now(),
		Status:      "running",
		IsSelfOp:    isSelfOp,
		Broadcaster: broadcaster,
	}

	s.operations[operationID] = operation
	s.activeOperations[stackName] = operationID
	s.mutex.Unlock()

	return operationID, nil
}

func (s *Service) isSelfOperation(stackName string, req OperationRequest) bool {
	if stackName != "berth-agent" {
		return false
	}

	if req.Command != "up" && req.Command != "restart" {
		return false
	}

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

	if operation.Broadcaster == nil {
		return fmt.Errorf("operation broadcaster not initialized")
	}

	s.mutex.RLock()
	if existingOpID, exists := s.activeOperations[operation.StackName]; exists && existingOpID != operationID {
		s.mutex.RUnlock()
		errorMsg := fmt.Sprintf("Another operation (%s) is already running on stack '%s'", existingOpID, operation.StackName)
		operation.Broadcaster.BroadcastError(errorMsg)
		return fmt.Errorf("%s", errorMsg)
	}
	s.mutex.RUnlock()

	subscriberID := fmt.Sprintf("sub-%d", time.Now().UnixNano())

	err := operation.Broadcaster.Subscribe(subscriberID, writer)
	if err != nil {
		return err
	}
	defer operation.Broadcaster.Unsubscribe(subscriberID)

	if operation.Broadcaster.IsStarted() {

		s.waitForCompletion(ctx, operation)
		return nil
	}

	operation.Broadcaster.MarkStarted()

	defer func() {
		s.mutex.Lock()
		if currentOpID, exists := s.activeOperations[operation.StackName]; exists && currentOpID == operationID {
			delete(s.activeOperations, operation.StackName)
		}
		s.mutex.Unlock()
	}()

	if operation.IsSelfOp {
		return s.handleSelfOperationWithBroadcast(ctx, operation)
	}

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, operation.StackName)
	if err != nil {
		operation.Broadcaster.BroadcastError(fmt.Sprintf("Invalid stack path: %v", err))
		return fmt.Errorf("invalid stack path: %w", err)
	}

	if operation.Request.Command == "create-archive" || operation.Request.Command == "extract-archive" {
		return s.handleArchiveOperationWithBroadcast(ctx, operation, stackPath)
	}

	var tempDockerConfig string
	if len(operation.Request.RegistryCredentials) > 0 {
		var err error
		tempDockerConfig, err = s.createTempDockerConfigWithBroadcast(ctx, operation.Request.RegistryCredentials, operation.Broadcaster)
		if err != nil {
			operation.Broadcaster.BroadcastError(fmt.Sprintf("Registry authentication failed: %v", err))
			s.updateOperationStatus(operationID, "failed", nil)
			return err
		}
		defer os.RemoveAll(tempDockerConfig)
	}

	cmd := s.buildCommand(operation.Request, stackPath)
	cmd.Dir = stackPath

	if tempDockerConfig != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_CONFIG=%s", tempDockerConfig))
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		operation.Broadcaster.BroadcastError(fmt.Sprintf("Failed to create stdout pipe: %v", err))
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		operation.Broadcaster.BroadcastError(fmt.Sprintf("Failed to create stderr pipe: %v", err))
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		operation.Broadcaster.BroadcastError(fmt.Sprintf("Failed to start command: %v", err))
		return err
	}

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		s.streamOutputToBroadcaster(ctx, stdout, operation.Broadcaster, StreamTypeStdout)
	}()

	go func() {
		defer wg.Done()
		s.streamOutputToBroadcaster(ctx, stderr, operation.Broadcaster, StreamTypeStderr)
	}()

	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode := exitError.ExitCode()
			s.updateOperationStatus(operationID, "completed", &exitCode)
			operation.Broadcaster.BroadcastComplete(false, exitCode)
		} else {
			s.updateOperationStatus(operationID, "failed", nil)
			operation.Broadcaster.BroadcastError(fmt.Sprintf("Command execution error: %v", err))
		}
	} else {
		exitCode := 0
		s.updateOperationStatus(operationID, "completed", &exitCode)
		operation.Broadcaster.BroadcastComplete(true, exitCode)
	}

	time.Sleep(2 * time.Second)

	return nil
}

func (s *Service) waitForCompletion(ctx context.Context, operation *Operation) error {

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			if operation.Broadcaster.IsCompleted() {
				return nil
			}
		}
	}
}

func (s *Service) streamOutputToBroadcaster(ctx context.Context, reader io.Reader, broadcaster *Broadcaster, streamType StreamMessageType) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			line := scanner.Text()
			broadcaster.Broadcast(streamType, line)
		}
	}
}

func (s *Service) handleSelfOperationWithBroadcast(ctx context.Context, operation *Operation) error {
	stackPath, err := validation.SanitizeStackPath(s.stackLocation, operation.StackName)
	if err != nil {
		s.updateOperationStatus(operation.ID, "failed", nil)
		operation.Broadcaster.BroadcastError(fmt.Sprintf("Invalid stack path: %v", err))
		return fmt.Errorf("invalid stack path: %w", err)
	}

	operation.Broadcaster.Broadcast(StreamTypeStdout, "Detected self-operation, forwarding to sidecar updater...")
	operation.Broadcaster.Broadcast(StreamTypeStdout, fmt.Sprintf("Sidecar will handle %s operation independently", operation.Request.Command))
	operation.Broadcaster.Broadcast(StreamTypeStdout, "Agent update will continue in background after this connection closes")

	exitCode := 0
	s.updateOperationStatus(operation.ID, "completed", &exitCode)
	operation.Broadcaster.BroadcastComplete(true, exitCode)

	time.Sleep(500 * time.Millisecond)

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	payload := map[string]any{
		"command":    operation.Request.Command,
		"options":    operation.Request.Options,
		"services":   operation.Request.Services,
		"stack_path": stackPath,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://berth-updater:8081/operation", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to sidecar: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sidecar returned status: %d", resp.StatusCode)
	}

	return nil
}

func (s *Service) handleArchiveOperationWithBroadcast(ctx context.Context, operation *Operation, stackPath string) error {
	progressWriter := NewBroadcasterProgressWriter(operation.Broadcaster)

	var err error
	switch operation.Request.Command {
	case "create-archive":
		err = s.handleCreateArchive(ctx, operation, stackPath, progressWriter)
	case "extract-archive":
		err = s.handleExtractArchive(ctx, operation, stackPath, progressWriter)
	default:
		s.updateOperationStatus(operation.ID, "failed", nil)
		operation.Broadcaster.BroadcastError(fmt.Sprintf("Unknown archive command: %s", operation.Request.Command))
		return fmt.Errorf("unknown archive command: %s", operation.Request.Command)
	}

	if err != nil {
		s.updateOperationStatus(operation.ID, "failed", nil)
		operation.Broadcaster.BroadcastError(fmt.Sprintf("Archive operation failed: %v", err))
		return err
	}

	exitCode := 0
	s.updateOperationStatus(operation.ID, "completed", &exitCode)
	operation.Broadcaster.Broadcast(StreamTypeStdout, "Archive operation completed successfully")
	operation.Broadcaster.BroadcastComplete(true, exitCode)

	return nil
}

func (s *Service) createTempDockerConfigWithBroadcast(ctx context.Context, credentials []RegistryCredential, broadcaster *Broadcaster) (string, error) {
	tempDir, err := os.MkdirTemp("", "berth-docker-config-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp docker config directory: %w", err)
	}

	if err := os.Chmod(tempDir, 0700); err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to set permissions on temp docker config: %w", err)
	}

	for _, cred := range credentials {
		broadcaster.Broadcast(StreamTypeProgress, fmt.Sprintf("Authenticating to %s...", cred.Registry))

		cmd := exec.CommandContext(ctx, "docker", "login", cred.Registry, "-u", cred.Username, "--password-stdin")
		cmd.Env = []string{
			fmt.Sprintf("DOCKER_CONFIG=%s", tempDir),
			"PATH=/usr/local/bin:/usr/bin:/bin",
			"HOME=/tmp",
		}
		cmd.Stdin = strings.NewReader(cred.Password)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			errorMsg := stderr.String()
			if errorMsg == "" {
				errorMsg = err.Error()
			}
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("docker login to %s failed: %s", cred.Registry, errorMsg)
		}

		broadcaster.Broadcast(StreamTypeProgress, fmt.Sprintf("Successfully authenticated to %s", cred.Registry))
	}

	return tempDir, nil
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

func (s *Service) updateOperationStatus(operationID, status string, exitCode *int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if op, exists := s.operations[operationID]; exists {
		op.Status = status
		op.ExitCode = exitCode
	}
}
