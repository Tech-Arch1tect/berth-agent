package websocket

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mutex      sync.RWMutex
	logger     *logging.Logger
}

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

func NewHub(logger *logging.Logger) *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		logger:     logger,
	}
}

func (h *Hub) Run() {
	h.logger.Info("WebSocket hub started")

	for {
		select {
		case client := <-h.register:
			h.mutex.Lock()
			h.clients[client] = true
			clientCount := len(h.clients)
			h.mutex.Unlock()

			h.logger.Debug("WebSocket client registered")
			h.logger.Info("WebSocket client connected",
				zap.Int("client_count", clientCount))

		case client := <-h.unregister:
			h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			clientCount := len(h.clients)
			h.mutex.Unlock()

			h.logger.Debug("WebSocket client unregistered")
			h.logger.Info("WebSocket client disconnected",
				zap.Int("client_count", clientCount))

		case message := <-h.broadcast:
			h.mutex.RLock()
			recipientCount := 0
			failedCount := 0

			for client := range h.clients {
				select {
				case client.send <- message:
					recipientCount++
				default:
					h.logger.Error("Failed to send message to client, removing client")
					delete(h.clients, client)
					close(client.send)
					failedCount++
				}
			}
			h.mutex.RUnlock()

			h.logger.Debug("Message broadcast completed",
				zap.Int("recipient_count", recipientCount),
				zap.Int("failed_count", failedCount))
		}
	}
}

func (h *Hub) BroadcastContainerStatus(event ContainerStatusEvent) {
	event.BaseMessage = BaseMessage{
		Type:      MessageTypeContainerStatus,
		Timestamp: event.Timestamp,
	}

	data, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("Failed to marshal container status event",
			zap.Error(err))
		return
	}

	h.logger.Debug("Broadcasting container status",
		zap.String("message_type", string(MessageTypeContainerStatus)))
	h.broadcast <- data
}

func (h *Hub) BroadcastStackStatus(event StackStatusEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("Failed to marshal stack status event",
			zap.Error(err))
		return
	}

	h.logger.Debug("Broadcasting stack status",
		zap.String("message_type", string(MessageTypeStackStatus)))
	h.broadcast <- data
}

func (h *Hub) BroadcastOperationProgress(event OperationProgressEvent) {
	event.BaseMessage = BaseMessage{
		Type:      MessageTypeOperationProgress,
		Timestamp: event.Timestamp,
	}

	data, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("Failed to marshal operation progress event",
			zap.Error(err))
		return
	}

	h.logger.Debug("Broadcasting operation progress",
		zap.String("message_type", string(MessageTypeOperationProgress)))
	h.broadcast <- data
}

func (h *Hub) ServeWebSocket(c echo.Context, authToken string) error {
	if authToken == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"error": "Authorization token required",
		})
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		h.logger.Error("WebSocket upgrade failed",
			zap.Error(err))
		return err
	}

	client := &Client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 256),
	}

	client.hub.register <- client

	go client.writePump()
	go client.readPump()

	return nil
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.logger.Warn("Unexpected WebSocket close error",
					zap.Error(err))
			}
			break
		}
	}
}

func (c *Client) writePump() {
	defer func() { _ = c.conn.Close() }()

	for message := range c.send {
		_ = c.conn.WriteMessage(websocket.TextMessage, message)
	}
	_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
}
