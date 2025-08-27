package operations

import (
	"berth-agent/internal/validation"
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	stackLocation string
	operations    map[string]*Operation
	mutex         sync.RWMutex
}

func NewService(stackLocation string) *Service {
	return &Service{
		stackLocation: stackLocation,
		operations:    make(map[string]*Operation),
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

	operationID := uuid.New().String()

	operation := &Operation{
		ID:        operationID,
		StackName: stackName,
		Request:   req,
		StartTime: time.Now(),
		Status:    "running",
	}

	s.mutex.Lock()
	s.operations[operationID] = operation
	s.mutex.Unlock()

	return operationID, nil
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

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, operation.StackName)
	if err != nil {
		return fmt.Errorf("invalid stack path: %w", err)
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

func (s *Service) streamOutput(reader io.Reader, writer io.Writer, streamType StreamMessageType) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		s.sendMessage(writer, streamType, line)
	}
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
			recover()
		}()
		flusher.Flush()
	}
}

func (s *Service) sendCompleteMessage(writer io.Writer, success bool, exitCode int) {
	output := fmt.Sprintf("data: {\"type\":\"complete\",\"success\":%t,\"exitCode\":%d}\n\n",
		success, exitCode)

	_, err := writer.Write([]byte(output))
	if err != nil {
		return
	}

	if flusher, ok := writer.(interface{ Flush() }); ok {
		defer func() {
			recover()
		}()
		flusher.Flush()
	}
}

func (s *Service) updateOperationStatus(operationID, status string, exitCode *int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if op, exists := s.operations[operationID]; exists {
		op.Status = status
		op.ExitCode = exitCode
	}
}
