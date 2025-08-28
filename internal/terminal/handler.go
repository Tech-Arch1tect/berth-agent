package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	ws "berth-agent/internal/websocket"
)

type Handler struct {
	manager      *Manager
	dockerClient *client.Client
}

type TerminalRequest struct {
	StackName     string `json:"stack_name"`
	ServiceName   string `json:"service_name"`
	ContainerName string `json:"container_name"`
	Cols          int    `json:"cols"`
	Rows          int    `json:"rows"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewHandler(dockerClient *client.Client) *Handler {
	return &Handler{
		manager:      NewManager(dockerClient),
		dockerClient: dockerClient,
	}
}

func (h *Handler) HandleTerminalWebSocket(c echo.Context) error {
	log.Printf("Terminal: New connection from %s", c.RealIP())

	authHeader := c.Request().Header.Get("Authorization")
	if authHeader == "" {
		log.Printf("Terminal: Missing authorization header")
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"error": "Authorization header required",
		})
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		log.Printf("Terminal: WebSocket upgrade failed: %v", err)
		return err
	}
	defer func() { _ = conn.Close() }()

	var session *Session
	sessionCreated := false

	for {
		var rawMessage json.RawMessage
		if err := conn.ReadJSON(&rawMessage); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Terminal: WebSocket read error: %v", err)
			}
			break
		}

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

			log.Printf("Terminal: Starting session for %s/%s", req.StackName, req.ServiceName)
			containerID, err := h.findContainerID(req.StackName, req.ServiceName, req.ContainerName)
			if err != nil {
				log.Printf("Terminal: Container not found: %v", err)
				h.sendError(conn, "Container not found", err.Error())
				continue
			}

			session, err = h.manager.CreateSession(req.StackName, req.ServiceName, containerID, req.Cols, req.Rows)
			if err != nil {
				log.Printf("Terminal: Failed to create session: %v", err)
				errorMsg := "Failed to create terminal session"
				if strings.Contains(err.Error(), "no compatible shell found") {
					errorMsg = "No compatible shell found in container"
				}
				h.sendError(conn, errorMsg, err.Error())
				continue
			}

			sessionCreated = true

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
					log.Printf("Terminal: Failed to send output: %v", err)
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

			log.Printf("Terminal: Session %s created successfully", session.ID)
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

				log.Printf("Terminal: Closing session %s", session.ID)
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
