package terminal

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"berth-agent/internal/compose"
	"berth-agent/internal/config"
	"berth-agent/internal/utils"

	"github.com/gorilla/websocket"
)

var (
	terminalManager *TerminalManager

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

func InitTerminalManager(timeout time.Duration) {
	terminalManager = NewTerminalManager(timeout)
}

func GetTerminalManager() *TerminalManager {
	return terminalManager
}

func TerminalSessionHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")

		pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		var service string
		if len(pathParts) >= 6 && pathParts[4] == "terminal" && pathParts[5] == "session" {
			if len(pathParts) > 6 {
				service = pathParts[6]
			}
		}

		if service == "" {
			http.Error(w, "Service name is required", http.StatusBadRequest)
			return
		}

		shell := r.URL.Query().Get("shell")
		if shell == "" {
			shell = "auto"
		}

		stackDir, _, err := compose.ValidateStackAndFindComposeFile(cfg, stackName)
		if err != nil {
			if err.Error() == "stack not found" {
				http.Error(w, err.Error(), http.StatusNotFound)
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			return
		}

		if r.Header.Get("Connection") != "Upgrade" || r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "WebSocket upgrade required", http.StatusBadRequest)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer ws.Close()

		sessionID := fmt.Sprintf("%s-%s-%d", stackName, service, time.Now().UnixNano())

		session, err := terminalManager.CreateSession(sessionID, stackName, service, stackDir, shell, ws)
		if err != nil {
			ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error creating terminal session: %v\r\n", err)))
			return
		}

		<-session.Done

		terminalManager.RemoveSession(sessionID)
	}
}
