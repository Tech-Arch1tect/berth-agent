package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

const (
	AuthStatusContextKey    = "auth_status"
	AuthErrorContextKey     = "auth_error"
	AuthTokenHashContextKey = "auth_token_hash"
)

func TokenMiddleware(accessToken string, logger *logging.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sourceIP := c.RealIP()

			if accessToken == "" {
				SetAuthFailure(c, "Access token not configured")
				logger.Warn("Authentication failed - token not configured",
					zap.String("auth_status", "failed"),
					zap.String("source_ip", sourceIP),
					zap.String("reason", "Access token not configured"))
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": "Access token not configured",
				})
			}

			auth := c.Request().Header.Get("Authorization")
			if auth == "" {
				SetAuthFailure(c, "Authorization header required")
				logger.Warn("Authentication failed - missing authorization header",
					zap.String("auth_status", "failed"),
					zap.String("source_ip", sourceIP))
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Authorization header required",
				})
			}

			if !strings.HasPrefix(auth, "Bearer ") {
				SetAuthFailure(c, "Bearer token required")
				logger.Warn("Authentication failed - invalid authorization format",
					zap.String("auth_status", "failed"),
					zap.String("source_ip", sourceIP))
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Bearer token required",
				})
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			logger.Debug("Validating token",
				zap.String("auth_status", "validating"),
				zap.String("source_ip", sourceIP),
				zap.String("token_hash", getTokenHash(token)))

			if subtle.ConstantTimeCompare([]byte(token), []byte(accessToken)) != 1 {
				SetAuthFailure(c, "Invalid token")
				logger.Warn("Authentication failed - invalid token",
					zap.String("auth_status", "failed"),
					zap.String("source_ip", sourceIP),
					zap.String("token_hash", getTokenHash(token)))
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Invalid token",
				})
			}

			SetAuthSuccess(c, token)
			logger.Info("Authentication successful",
				zap.String("auth_status", "success"),
				zap.String("source_ip", sourceIP),
				zap.String("token_hash", getTokenHash(token)))
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
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])[:16]
}

func getTokenHash(token string) string {
	return HashToken(token)
}
