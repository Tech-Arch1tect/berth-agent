package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	ws "github.com/tech-arch1tect/berth-agent/internal/websocket"
)

type Handler struct {
	manager      *Manager
	dockerClient *client.Client
	auditLog     *logging.Service
	logger       *logging.Logger
}

type TerminalRequest struct {
	StackName     string `json:"stack_name"`
	ServiceName   string `json:"service_name"`
	ContainerName string `json:"container_name"`
	Cols          int    `json:"cols"`
	Rows          int    `json:"rows"`
}

const (
	terminalPingInterval = 30 * time.Second
	terminalPongWait     = 60 * time.Second
	terminalWriteWait    = 10 * time.Second
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewHandler(dockerClient *client.Client, auditLog *logging.Service, logger *logging.Logger) *Handler {
	return &Handler{
		manager:      NewManager(dockerClient, logger),
		dockerClient: dockerClient,
		auditLog:     auditLog,
		logger:       logger,
	}
}

func (h *Handler) HandleTerminalWebSocket(c echo.Context) error {
	h.logger.Info("New WebSocket connection for terminal",
		zap.String("source_ip", c.RealIP()),
		zap.String("user_agent", c.Request().UserAgent()),
	)

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		h.logger.Error("WebSocket upgrade failed",
			zap.String("source_ip", c.RealIP()),
			zap.Error(err),
		)
		return err
	}

	done := make(chan struct{})

	defer func() {
		select {
		case <-done:
		default:
			close(done)
		}
		_ = conn.Close()
		h.logger.Info("WebSocket connection closed",
			zap.String("source_ip", c.RealIP()),
		)
	}()

	_ = conn.SetReadDeadline(time.Now().Add(terminalPongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(terminalPongWait))
		return nil
	})

	go func() {
		ticker := time.NewTicker(terminalPingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(terminalWriteWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					h.logger.Debug("Ping failed, connection likely closed",
						zap.String("source_ip", c.RealIP()),
						zap.Error(err),
					)
					return
				}
			case <-done:
				return
			}
		}
	}()

	var session *Session
	sessionCreated := false

	for {
		var rawMessage json.RawMessage
		if err := conn.ReadJSON(&rawMessage); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.logger.Error("WebSocket read error",
					zap.String("source_ip", c.RealIP()),
					zap.Error(err),
				)
			}
			break
		}

		_ = conn.SetReadDeadline(time.Now().Add(terminalPongWait))

		var baseMsg ws.BaseMessage
		if err := json.Unmarshal(rawMessage, &baseMsg); err != nil {
			h.sendError(conn, "Invalid message format", err.Error())
			continue
		}

		switch baseMsg.Type {
		case "terminal_start":
			if sessionCreated {
				h.sendError(conn, "Session already created", "")
				continue
			}

			var req TerminalRequest
			if err := json.Unmarshal(rawMessage, &req); err != nil {
				h.sendError(conn, "Invalid terminal start request", err.Error())
				continue
			}

			h.logger.Info("Starting terminal session",
				zap.String("stack_name", req.StackName),
				zap.String("service_name", req.ServiceName),
				zap.String("container_name", req.ContainerName),
			)

			containerID, err := h.findContainerID(req.StackName, req.ServiceName, req.ContainerName)
			if err != nil {
				h.logger.Error("Container not found",
					zap.String("stack_name", req.StackName),
					zap.String("service_name", req.ServiceName),
					zap.String("container_name", req.ContainerName),
					zap.Error(err),
				)
				h.sendError(conn, "Container not found", err.Error())
				continue
			}

			session, err = h.manager.CreateSession(req.StackName, req.ServiceName, containerID, req.Cols, req.Rows)
			if err != nil {
				h.logger.Error("Failed to create terminal session",
					zap.String("stack_name", req.StackName),
					zap.String("service_name", req.ServiceName),
					zap.String("container_id", containerID),
					zap.Error(err),
				)
				errorMsg := "Failed to create terminal session"
				if strings.Contains(err.Error(), "no compatible shell found") {
					errorMsg = "No compatible shell found in container"
				}
				h.sendError(conn, errorMsg, err.Error())
				continue
			}

			sessionCreated = true

			h.logTerminalSession(c, req, session.ID, true, "")

			session.SetOutputCallback(func(output []byte) {
				event := ws.TerminalOutputEvent{
					BaseMessage: ws.BaseMessage{
						Type:      ws.MessageTypeTerminalOutput,
						Timestamp: time.Now().Format(time.RFC3339),
					},
					SessionID: session.ID,
					Output:    output,
				}
				if err := conn.WriteJSON(event); err != nil {
					h.logger.Error("Failed to send terminal output to WebSocket",
						zap.String("session_id", session.ID),
						zap.Error(err),
					)
				}
			})

			session.SetCloseCallback(func(exitCode int) {
				event := ws.TerminalCloseEvent{
					BaseMessage: ws.BaseMessage{
						Type:      ws.MessageTypeTerminalClose,
						Timestamp: time.Now().Format(time.RFC3339),
					},
					SessionID: session.ID,
					ExitCode:  exitCode,
				}
				_ = conn.WriteJSON(event)
				_ = conn.Close()
			})

			h.logger.Info("Terminal session started successfully",
				zap.String("session_id", session.ID),
				zap.String("stack_name", req.StackName),
				zap.String("service_name", req.ServiceName),
			)
			h.sendSuccess(conn, "Terminal session started", session.ID)

		case ws.MessageTypeTerminalInput:
			if !sessionCreated || session == nil {
				h.sendError(conn, "No active session", "")
				continue
			}

			var inputEvent ws.TerminalInputEvent
			if err := json.Unmarshal(rawMessage, &inputEvent); err != nil {
				h.sendError(conn, "Invalid input event", err.Error())
				continue
			}

			if inputEvent.SessionID != session.ID {
				h.sendError(conn, "Session ID mismatch", "")
				continue
			}

			if err := session.Write(inputEvent.Input); err != nil {
				h.logger.Error("Failed to write input to terminal",
					zap.String("session_id", session.ID),
					zap.Error(err),
				)
				h.sendError(conn, "Failed to write to terminal", err.Error())
				continue
			}

		case ws.MessageTypeTerminalResize:
			if !sessionCreated || session == nil {
				h.sendError(conn, "No active session", "")
				continue
			}

			var resizeEvent ws.TerminalResizeEvent
			if err := json.Unmarshal(rawMessage, &resizeEvent); err != nil {
				h.sendError(conn, "Invalid resize event", err.Error())
				continue
			}

			if resizeEvent.SessionID != session.ID {
				h.sendError(conn, "Session ID mismatch", "")
				continue
			}

			if err := session.Resize(resizeEvent.Cols, resizeEvent.Rows); err != nil {
				h.logger.Error("Failed to resize terminal",
					zap.String("session_id", session.ID),
					zap.Int("cols", resizeEvent.Cols),
					zap.Int("rows", resizeEvent.Rows),
					zap.Error(err),
				)
				h.sendError(conn, "Failed to resize terminal", err.Error())
				continue
			}

		case ws.MessageTypeTerminalClose:
			if sessionCreated && session != nil {
				var closeEvent ws.TerminalCloseEvent
				if err := json.Unmarshal(rawMessage, &closeEvent); err == nil {
					if closeEvent.SessionID != session.ID {
						h.sendError(conn, "Session ID mismatch", "")
						continue
					}
				}

				h.logger.Info("Client requested terminal session close",
					zap.String("session_id", session.ID),
				)
				_ = h.manager.CloseSession(session.ID)
				session = nil
			}
			time.Sleep(100 * time.Millisecond)
			return nil

		default:
			h.sendError(conn, "Unknown message type", string(baseMsg.Type))
		}
	}

	if sessionCreated && session != nil {
		_ = h.manager.CloseSession(session.ID)
	}

	return nil
}

func (h *Handler) findContainerID(stackName, serviceName, containerName string) (string, error) {
	ctx := context.Background()

	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "com.docker.compose.project="+stackName)
	filterArgs.Add("label", "com.docker.compose.service="+serviceName)

	if containerName != "" {
		filterArgs.Add("name", containerName)
	}

	containers, err := h.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return "", fmt.Errorf("no containers found for stack=%s, service=%s, container=%s",
			stackName, serviceName, containerName)
	}

	for _, c := range containers {
		if c.State == "running" {
			if err := h.validateContainerStackMatch(c.ID, stackName); err != nil {
				return "", fmt.Errorf("container validation failed: %w", err)
			}
			return c.ID, nil
		}
	}

	return "", fmt.Errorf("no running containers found for stack=%s, service=%s, container=%s",
		stackName, serviceName, containerName)
}

func (h *Handler) validateContainerStackMatch(containerID, expectedStackName string) error {
	ctx := context.Background()

	inspect, err := h.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	actualStackName, exists := inspect.Config.Labels["com.docker.compose.project"]
	if !exists {
		return fmt.Errorf("container has no Docker Compose project label")
	}

	if actualStackName != expectedStackName {
		return fmt.Errorf("stack mismatch: container belongs to '%s' but claimed stack is '%s'",
			actualStackName, expectedStackName)
	}

	return nil
}

func (h *Handler) logTerminalSession(c echo.Context, req TerminalRequest, sessionID string, success bool, errorMsg string) {
	if h.auditLog == nil {
		return
	}

	entry := logging.NewRequestLogEntry()
	entry.RequestID = c.Response().Header().Get("X-Request-ID")
	entry.Method = "WEBSOCKET"
	entry.Path = "/ws/terminal"
	entry.SourceIP = c.RealIP()
	entry.UserAgent = c.Request().UserAgent()
	entry.StackName = req.StackName
	entry.Metadata["action"] = "terminal_session_created"
	entry.Metadata["session_id"] = sessionID
	entry.Metadata["service_name"] = req.ServiceName

	if req.ContainerName != "" {
		entry.ContainerName = req.ContainerName
	}

	if authTokenHash, ok := c.Get("auth_token_hash").(string); ok {
		entry.AuthTokenHash = authTokenHash
	}
	entry.AuthStatus = "success"

	if success {
		entry.StatusCode = 200
	} else {
		entry.StatusCode = 500
		entry.Error = errorMsg
	}

	h.auditLog.LogRequest(entry)
}

func (h *Handler) sendError(conn *websocket.Conn, message, details string) {
	event := ws.ErrorEvent{
		BaseMessage: ws.BaseMessage{
			Type:      ws.MessageTypeError,
			Timestamp: time.Now().Format(time.RFC3339),
		},
		Error:   message,
		Context: details,
	}
	_ = conn.WriteJSON(event)
}

func (h *Handler) sendSuccess(conn *websocket.Conn, message, sessionID string) {
	response := map[string]any{
		"type":       "success",
		"message":    message,
		"session_id": sessionID,
		"timestamp":  time.Now().Format(time.RFC3339),
	}
	_ = conn.WriteJSON(response)
}

func (h *Handler) Shutdown() {
	h.manager.CloseAllSessions()
}
