package websocket

import (
	"strings"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	hub         *Hub
	accessToken string
}

func NewHandler(hub *Hub, accessToken string) *Handler {
	return &Handler{
		hub:         hub,
		accessToken: accessToken,
	}
}

func (h *Handler) HandleAgentWebSocket(c echo.Context) error {
	auth := c.Request().Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return c.JSON(401, map[string]string{
			"error": "Bearer token required",
		})
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	if token != h.accessToken {
		return c.JSON(401, map[string]string{
			"error": "Invalid token",
		})
	}

	return h.hub.ServeWebSocket(c, token)
}
