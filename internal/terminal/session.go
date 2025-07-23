package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type TerminalSession struct {
	ID           string
	StackName    string
	Service      string
	PTY          *os.File
	Cmd          *exec.Cmd
	WebSocket    *websocket.Conn
	Done         chan struct{}
	mu           sync.Mutex
	closed       bool
	lastActivity time.Time
}

type TerminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

type TerminalManager struct {
	sessions map[string]*TerminalSession
	mu       sync.RWMutex
	timeout  time.Duration
}

func NewTerminalManager(timeout time.Duration) *TerminalManager {
	tm := &TerminalManager{
		sessions: make(map[string]*TerminalSession),
		timeout:  timeout,
	}

	go tm.cleanupSessions()

	return tm
}

func (tm *TerminalManager) CreateSession(sessionID, stackName, service, stackDir, shell string, ws *websocket.Conn) (*TerminalSession, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.sessions[sessionID]; exists {
		return nil, fmt.Errorf("session %s already exists", sessionID)
	}

	var cmd *exec.Cmd

	switch shell {
	case "bash":
		cmd = exec.Command("docker", "compose", "exec", "-it", service, "bash")
	case "sh":
		cmd = exec.Command("docker", "compose", "exec", "-it", service, "sh")
	case "zsh":
		cmd = exec.Command("docker", "compose", "exec", "-it", service, "zsh")
	case "fish":
		cmd = exec.Command("docker", "compose", "exec", "-it", service, "fish")
	case "dash":
		cmd = exec.Command("docker", "compose", "exec", "-it", service, "dash")
	case "auto":
		fallthrough
	default:
		cmd = exec.Command("docker", "compose", "exec", "-it", service, "sh", "-c", "bash || sh")
	}

	cmd.Dir = stackDir

	ptyFile, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	session := &TerminalSession{
		ID:           sessionID,
		StackName:    stackName,
		Service:      service,
		PTY:          ptyFile,
		Cmd:          cmd,
		WebSocket:    ws,
		Done:         make(chan struct{}),
		lastActivity: time.Now(),
	}

	tm.sessions[sessionID] = session

	go session.handlePTYOutput()
	go session.handleWebSocketInput()
	go session.waitForCompletion()

	return session, nil
}

func (tm *TerminalManager) GetSession(sessionID string) (*TerminalSession, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	session, exists := tm.sessions[sessionID]
	return session, exists
}

func (tm *TerminalManager) RemoveSession(sessionID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if session, exists := tm.sessions[sessionID]; exists {
		session.Close()
		delete(tm.sessions, sessionID)
	}
}

func (tm *TerminalManager) cleanupSessions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		tm.mu.Lock()
		now := time.Now()
		for sessionID, session := range tm.sessions {
			session.mu.Lock()
			if now.Sub(session.lastActivity) > tm.timeout {
				log.Printf("Cleaning up inactive session: %s", sessionID)
				session.Close()
				delete(tm.sessions, sessionID)
			}
			session.mu.Unlock()
		}
		tm.mu.Unlock()
	}
}

func (ts *TerminalSession) handlePTYOutput() {
	defer func() {
		ts.safeClose()
	}()

	buffer := make([]byte, 1024)
	for {
		select {
		case <-ts.Done:
			return
		default:
			n, err := ts.PTY.Read(buffer)
			if err != nil {
				if err != io.EOF {
					log.Printf("PTY read error for session %s: %v", ts.ID, err)
				}
				return
			}

			ts.updateActivity()

			if err := ts.WebSocket.WriteMessage(websocket.TextMessage, buffer[:n]); err != nil {
				log.Printf("WebSocket write error for session %s: %v", ts.ID, err)
				return
			}
		}
	}
}

func (ts *TerminalSession) handleWebSocketInput() {
	defer ts.Close()

	for {
		select {
		case <-ts.Done:
			return
		default:
			_, messageData, err := ts.WebSocket.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.Printf("WebSocket read error for session %s: %v", ts.ID, err)
				}
				return
			}

			ts.updateActivity()

			var msg TerminalMessage
			if err := json.Unmarshal(messageData, &msg); err != nil {
				log.Printf("Invalid terminal message for session %s: %v", ts.ID, err)
				continue
			}

			switch msg.Type {
			case "input":
				if _, err := ts.PTY.Write([]byte(msg.Data)); err != nil {
					log.Printf("PTY write error for session %s: %v", ts.ID, err)
					return
				}
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					if err := pty.Setsize(ts.PTY, &pty.Winsize{
						Rows: uint16(msg.Rows),
						Cols: uint16(msg.Cols),
					}); err != nil {
						log.Printf("PTY resize error for session %s: %v", ts.ID, err)
					}
				}
			case "close":
				return
			}
		}
	}
}

func (ts *TerminalSession) waitForCompletion() {
	if ts.Cmd != nil && ts.Cmd.Process != nil {
		ts.Cmd.Wait()
	}
	ts.safeClose()
}

func (ts *TerminalSession) updateActivity() {
	ts.mu.Lock()
	ts.lastActivity = time.Now()
	ts.mu.Unlock()
}

func (ts *TerminalSession) safeClose() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !ts.closed {
		close(ts.Done)
		ts.closed = true
	}
}

func (ts *TerminalSession) Close() {
	ts.safeClose()

	if ts.PTY != nil {
		ts.PTY.Close()
	}

	if ts.Cmd != nil && ts.Cmd.Process != nil {
		ts.Cmd.Process.Signal(os.Interrupt)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		done := make(chan error, 1)
		go func() {
			done <- ts.Cmd.Wait()
		}()

		select {
		case <-ctx.Done():
			if ts.Cmd.Process != nil {
				ts.Cmd.Process.Kill()
			}
		case <-done:
		}
	}

	if ts.WebSocket != nil {
		ts.WebSocket.Close()
	}
}
