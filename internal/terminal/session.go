package terminal

import (
	"berth-agent/internal/logging"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Session struct {
	ID           string
	StackName    string
	ServiceName  string
	ContainerID  string
	ExecID       string
	dockerClient *client.Client
	hijackedResp *types.HijackedResponse
	ctx          context.Context
	cancel       context.CancelFunc
	mutex        sync.RWMutex
	closed       bool
	onOutput     func([]byte)
	onClose      func(int)
	cols         int
	rows         int
	logger       *logging.Logger
}

type Manager struct {
	sessions     map[string]*Session
	dockerClient *client.Client
	mutex        sync.RWMutex
	logger       *logging.Logger
}

func NewManager(dockerClient *client.Client, logger *logging.Logger) *Manager {
	return &Manager{
		sessions:     make(map[string]*Session),
		dockerClient: dockerClient,
		logger:       logger,
	}
}

func (m *Manager) CreateSession(stackName, serviceName, containerID string, cols, rows int) (*Session, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	sessionID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())

	session := &Session{
		ID:           sessionID,
		StackName:    stackName,
		ServiceName:  serviceName,
		ContainerID:  containerID,
		dockerClient: m.dockerClient,
		ctx:          ctx,
		cancel:       cancel,
		logger:       m.logger,
	}

	m.logger.Info("Creating terminal session",
		zap.String("session_id", sessionID),
		zap.String("stack_name", stackName),
		zap.String("service_name", serviceName),
		zap.String("container_id", containerID),
		zap.Int("cols", cols),
		zap.Int("rows", rows),
	)

	shells := [][]string{
		{"/bin/bash", "-l"},
		{"/bin/bash"},
		{"/bin/sh"},
		{"/bin/ash"},
		{"/bin/dash"},
		{"sh"},
	}

	var selectedShell []string
	var execIDResp container.ExecCreateResponse

	for _, shell := range shells {

		testShell := append(shell, "-c", "echo test")
		testConfig := container.ExecOptions{
			AttachStdout: true,
			AttachStderr: true,
			Cmd:          testShell,
		}

		testResp, err := m.dockerClient.ContainerExecCreate(ctx, containerID, testConfig)
		if err != nil {
			continue
		}

		testAttach, err := m.dockerClient.ContainerExecAttach(ctx, testResp.ID, container.ExecStartOptions{})
		if err != nil {
			continue
		}
		testAttach.Close()

		time.Sleep(100 * time.Millisecond)

		testInspect, err := m.dockerClient.ContainerExecInspect(ctx, testResp.ID)
		if err != nil || testInspect.ExitCode != 0 {
			continue
		}

		execConfig := container.ExecOptions{
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          true,
			ConsoleSize:  &[2]uint{uint(rows), uint(cols)},
			Cmd:          shell,
		}

		resp, err := m.dockerClient.ContainerExecCreate(ctx, containerID, execConfig)
		if err != nil {
			continue
		}

		selectedShell = shell
		execIDResp = resp
		m.logger.Info("Shell selected for terminal session",
			zap.String("session_id", sessionID),
			zap.Strings("shell", shell),
		)
		break
	}

	if len(selectedShell) == 0 {
		cancel()
		m.logger.Error("No compatible shell found in container",
			zap.String("session_id", sessionID),
			zap.String("container_id", containerID),
		)
		return nil, fmt.Errorf("no compatible shell found in container (tried: bash, sh, ash, dash)")
	}

	session.ExecID = execIDResp.ID

	execStartConfig := container.ExecStartOptions{
		Tty: true,
	}

	hijackedResp, err := m.dockerClient.ContainerExecAttach(ctx, execIDResp.ID, execStartConfig)
	if err != nil {
		cancel()
		m.logger.Error("Failed to attach to exec session",
			zap.String("session_id", sessionID),
			zap.String("exec_id", execIDResp.ID),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}

	m.logger.Info("PTY allocated and attached",
		zap.String("session_id", sessionID),
		zap.String("exec_id", execIDResp.ID),
	)

	session.hijackedResp = &hijackedResp
	session.cols = cols
	session.rows = rows

	go func() {
		defer func() {
			hijackedResp.Close()
			session.closeSession(0)
		}()

		outputReceived := false
		timeoutTimer := time.NewTimer(5 * time.Second)

		go func() {
			<-timeoutTimer.C
			if !outputReceived {
				session.mutex.RLock()
				callback := session.onOutput
				session.mutex.RUnlock()

				if callback != nil {
					errorMsg := fmt.Sprintf("\r\n\x1b[31mTerminal Error: Shell may not be available in this container.\r\nTried shells: %v\x1b[0m\r\n", selectedShell)
					callback([]byte(errorMsg))
				}
			}
		}()

		buf := make([]byte, 1024)
		for {
			n, err := hijackedResp.Reader.Read(buf)
			if err != nil {
				if err != io.EOF {
					session.logger.Error("Terminal I/O read error",
						zap.String("session_id", sessionID),
						zap.Error(err),
					)
				}
				return
			}

			if n > 0 {
				if !outputReceived {
					outputReceived = true
					timeoutTimer.Stop()
				}

				session.logger.Debug("Terminal output received",
					zap.String("session_id", sessionID),
					zap.Int("bytes", n),
				)

				session.mutex.RLock()
				callback := session.onOutput
				session.mutex.RUnlock()

				if callback != nil {
					callback(buf[:n])
				}
			}
		}
	}()

	m.sessions[sessionID] = session
	m.logger.Info("Terminal session created successfully",
		zap.String("session_id", sessionID),
		zap.String("stack_name", stackName),
		zap.String("service_name", serviceName),
	)
	return session, nil
}

func (m *Manager) GetSession(sessionID string) (*Session, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	session, exists := m.sessions[sessionID]
	return session, exists
}

func (m *Manager) CloseSession(sessionID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		m.logger.Warn("Attempted to close non-existent session",
			zap.String("session_id", sessionID),
		)
		return fmt.Errorf("session not found: %s", sessionID)
	}

	m.logger.Info("Closing terminal session",
		zap.String("session_id", sessionID),
		zap.String("stack_name", session.StackName),
		zap.String("service_name", session.ServiceName),
	)

	session.closeSession(0)
	delete(m.sessions, sessionID)
	return nil
}

func (m *Manager) CloseAllSessions() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.logger.Info("Closing all terminal sessions",
		zap.Int("session_count", len(m.sessions)),
	)

	for sessionID, session := range m.sessions {
		session.closeSession(0)
		delete(m.sessions, sessionID)
	}
}

func (s *Session) Write(data []byte) error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if s.closed || s.hijackedResp == nil {
		return fmt.Errorf("session is closed")
	}

	s.logger.Debug("Writing to terminal",
		zap.String("session_id", s.ID),
		zap.Int("bytes", len(data)),
	)

	_, err := s.hijackedResp.Conn.Write(data)
	if err != nil {
		s.logger.Error("Terminal I/O write error",
			zap.String("session_id", s.ID),
			zap.Error(err),
		)
	}
	return err
}

func (s *Session) Resize(cols, rows int) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.closed {
		return fmt.Errorf("session is closed")
	}

	s.logger.Debug("Resizing terminal",
		zap.String("session_id", s.ID),
		zap.Int("cols", cols),
		zap.Int("rows", rows),
	)

	s.cols = cols
	s.rows = rows

	if s.ExecID == "" {
		return fmt.Errorf("no exec session to resize")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resizeOptions := container.ResizeOptions{
		Height: uint(rows),
		Width:  uint(cols),
	}

	if err := s.dockerClient.ContainerExecResize(ctx, s.ExecID, resizeOptions); err != nil {
		s.logger.Error("Terminal resize failed",
			zap.String("session_id", s.ID),
			zap.Int("cols", cols),
			zap.Int("rows", rows),
			zap.Error(err),
		)
		return fmt.Errorf("failed to resize terminal: %w", err)
	}

	return nil
}

func (s *Session) SetOutputCallback(callback func([]byte)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.onOutput = callback
}

func (s *Session) SetCloseCallback(callback func(int)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.onClose = callback
}

func (s *Session) closeSession(exitCode int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.closed {
		return
	}

	s.logger.Info("Closing terminal session",
		zap.String("session_id", s.ID),
		zap.Int("exit_code", exitCode),
	)

	s.closed = true

	if s.hijackedResp != nil {
		s.hijackedResp.Close()
	}

	if s.cancel != nil {
		s.cancel()
	}

	if s.onClose != nil {
		go s.onClose(exitCode)
	}

	if s.ExecID != "" && s.dockerClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.dockerClient.ContainerExecStart(ctx, s.ExecID, container.ExecStartOptions{}); err != nil {
			s.logger.Warn("Failed to terminate exec session during cleanup",
				zap.String("session_id", s.ID),
				zap.String("exec_id", s.ExecID),
				zap.Error(err),
			)
		}
	}

	s.logger.Info("Terminal session closed",
		zap.String("session_id", s.ID),
		zap.String("stack_name", s.StackName),
		zap.String("service_name", s.ServiceName),
	)
}
