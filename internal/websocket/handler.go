package websocket

import (
	"github.com/labstack/echo/v4"
)

type Handler struct {
	hub *Hub
}

func NewHandler(hub *Hub) *Handler {
	return &Handler{
		hub: hub,
	}
}

func (h *Handler) HandleAgentWebSocket(c echo.Context) error {
	return h.hub.ServeWebSocket(c)
}
