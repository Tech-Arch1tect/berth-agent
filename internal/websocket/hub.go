package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
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
}

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mutex.Lock()
			h.clients[client] = true
			h.mutex.Unlock()
			log.Printf("WebSocket client connected. Total clients: %d", len(h.clients))

		case client := <-h.unregister:
			h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mutex.Unlock()
			log.Printf("WebSocket client disconnected. Total clients: %d", len(h.clients))

		case message := <-h.broadcast:
			h.mutex.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					delete(h.clients, client)
					close(client.send)
				}
			}
			h.mutex.RUnlock()
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
		log.Printf("Error marshalling container status event: %v", err)
		return
	}

	h.broadcast <- data
}

func (h *Hub) BroadcastStackStatus(event StackStatusEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error marshalling stack status event: %v", err)
		return
	}

	h.broadcast <- data
}

func (h *Hub) BroadcastOperationProgress(event OperationProgressEvent) {
	event.BaseMessage = BaseMessage{
		Type:      MessageTypeOperationProgress,
		Timestamp: event.Timestamp,
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error marshalling operation progress event: %v", err)
		return
	}

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
		log.Printf("WebSocket upgrade error: %v", err)
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
				log.Printf("WebSocket error: %v", err)
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
