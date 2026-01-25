package websocket

import (
	"github.com/tech-arch1tect/berth-agent/internal/auth"
	"github.com/tech-arch1tect/berth-agent/internal/common"
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
	authHeader := c.Request().Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		auth.SetAuthFailure(c, "Bearer token required")
		return common.SendUnauthorized(c, "Bearer token required")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token != h.accessToken {
		auth.SetAuthFailure(c, "Invalid token")
		return common.SendUnauthorized(c, "Invalid token")
	}

	auth.SetAuthSuccess(c, token)
	return h.hub.ServeWebSocket(c, token)
}
