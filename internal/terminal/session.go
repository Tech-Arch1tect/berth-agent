package terminal

import (
	"context"
	"fmt"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
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
	shell        []string
	dockerClient *client.Client
	hijackedResp *types.HijackedResponse
	ctx          context.Context
	cancel       context.CancelFunc
	mutex        sync.RWMutex
	started      bool
	closed       bool
	onOutput     func([]byte)
	onClose      func(*int, error)
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
	session.shell = selectedShell

	m.sessions[sessionID] = session
	m.logger.Info("Terminal session created successfully",
		zap.String("session_id", sessionID),
		zap.String("stack_name", stackName),
		zap.String("service_name", serviceName),
	)
	return session, nil
}

func (s *Session) Start() {
	s.mutex.Lock()
	if s.closed {
		callback := s.onClose
		s.mutex.Unlock()
		go callback(nil, nil)
		return
	}
	s.started = true
	s.mutex.Unlock()
	go s.readOutput()
}

func (s *Session) readOutput() {
	defer s.hijackedResp.Close()

	firstOutput := make(chan struct{})
	go warnWhenSilent(s.ctx, firstOutput, shellStartupWait, func() {
		s.mutex.RLock()
		callback := s.onOutput
		s.mutex.RUnlock()

		if callback != nil {
			errorMsg := fmt.Sprintf("\r\n\x1b[31mTerminal Error: Shell may not be available in this container.\r\nTried shells: %v\x1b[0m\r\n", s.shell)
			callback([]byte(errorMsg))
		}
	})

	announced := false
	buf := make([]byte, 1024)
	for {
		n, err := s.hijackedResp.Reader.Read(buf)
		if err != nil {
			if err == io.EOF {
				exitCode, inspectErr := s.waitForExec()
				s.closeSession(exitCode, inspectErr)
			} else {
				s.logger.Error("Terminal I/O read error",
					zap.String("session_id", s.ID),
					zap.Error(err),
				)
				s.closeSession(nil, fmt.Errorf("terminal output read: %w", err))
			}
			return
		}

		if n > 0 {
			if !announced {
				announced = true
				close(firstOutput)
			}

			s.logger.Debug("Terminal output received",
				zap.String("session_id", s.ID),
				zap.Int("bytes", n),
			)

			s.mutex.RLock()
			callback := s.onOutput
			s.mutex.RUnlock()

			if callback != nil {
				callback(buf[:n])
			}
		}
	}
}

func (s *Session) waitForExec() (*int, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	var ticker *time.Ticker
	for {
		inspect, err := s.dockerClient.ContainerExecInspect(ctx, s.ExecID)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect terminal exec: %w", err)
		}
		if !inspect.Running {
			exitCode := inspect.ExitCode
			return &exitCode, nil
		}
		if ticker == nil {
			ticker = time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("terminal exec result unavailable: %w", ctx.Err())
		case <-ticker.C:
		}
	}
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

	session.closeSession(nil, nil)
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
		session.closeSession(nil, nil)
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

func (s *Session) SetCloseCallback(callback func(*int, error)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.onClose = callback
}

func (s *Session) closeSession(exitCode *int, failure error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.closed {
		return
	}

	s.logger.Info("Closing terminal session", zap.String("session_id", s.ID))

	s.closed = true

	if s.hijackedResp != nil {
		s.hijackedResp.Close()
	}

	if s.cancel != nil {
		s.cancel()
	}

	if s.started && s.onClose != nil {
		go s.onClose(exitCode, failure)
	}

	s.logger.Info("Terminal session closed",
		zap.String("session_id", s.ID),
		zap.String("stack_name", s.StackName),
		zap.String("service_name", s.ServiceName),
	)
}
