package auth

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

const (
	AuthStatusContextKey    = "auth_status"
	AuthErrorContextKey     = "auth_error"
	AuthTokenHashContextKey = "auth_token_hash"
)

func TokenMiddleware(accessToken string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if accessToken == "" {
				SetAuthFailure(c, "Access token not configured")
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": "Access token not configured",
				})
			}

			auth := c.Request().Header.Get("Authorization")
			if auth == "" {
				SetAuthFailure(c, "Authorization header required")
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Authorization header required",
				})
			}

			if !strings.HasPrefix(auth, "Bearer ") {
				SetAuthFailure(c, "Bearer token required")
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Bearer token required",
				})
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			if token != accessToken {
				SetAuthFailure(c, "Invalid token")
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Invalid token",
				})
			}

			SetAuthSuccess(c, token)
			return next(c)
		}
	}
}

func SetAuthSuccess(c echo.Context, token string) {
	c.Set(AuthStatusContextKey, "success")
	if token != "" {
		c.Set(AuthTokenHashContextKey, HashToken(token))
	}
}

func SetAuthFailure(c echo.Context, reason string) {
	c.Set(AuthStatusContextKey, "failed")
	c.Set(AuthErrorContextKey, reason)
}

func HashToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "***"
	}
	return token[:4] + "..." + token[len(token)-4:]
}
