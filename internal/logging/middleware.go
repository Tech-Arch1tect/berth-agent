package logging

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const (
	AuthStatusSuccess       = "success"
	AuthStatusFailed        = "failed"
	AuthStatusNone          = "none"
	AuthStatusSkipped       = "skipped"
	RequestIDHeader         = "X-Request-ID"
	AuthStatusContextKey    = "auth_status"
	AuthErrorContextKey     = "auth_error"
	AuthTokenHashContextKey = "auth_token_hash"
)

func RequestLoggingMiddleware(service *Service) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if service == nil || !service.enabled {
				return next(c)
			}

			start := time.Now()
			path := c.Request().URL.Path

			requestID := c.Request().Header.Get(RequestIDHeader)
			if requestID == "" {
				requestID = uuid.New().String()
				c.Request().Header.Set(RequestIDHeader, requestID)
			}
			c.Response().Header().Set(RequestIDHeader, requestID)

			entry := NewRequestLogEntry()
			entry.RequestID = requestID
			entry.Method = c.Request().Method
			entry.Path = path
			entry.UserAgent = c.Request().UserAgent()

			sourceIP := c.RealIP()
			if sourceIP == "" {
				sourceIP = c.Request().RemoteAddr
			}
			entry.SourceIP = sourceIP

			err := next(c)

			extractPathParams(c, entry)

			entry.StatusCode = c.Response().Status
			entry.ResponseSize = c.Response().Size
			entry.LatencyMs = float64(time.Since(start).Microseconds()) / 1000.0

			if authStatus, ok := c.Get(AuthStatusContextKey).(string); ok {
				entry.AuthStatus = authStatus
			}
			if authError, ok := c.Get(AuthErrorContextKey).(string); ok {
				entry.AuthError = authError
			}
			if authTokenHash, ok := c.Get(AuthTokenHashContextKey).(string); ok {
				entry.AuthTokenHash = authTokenHash
			}

			if err != nil {
				if he, ok := err.(*echo.HTTPError); ok {
					entry.Error = fmt.Sprintf("%v", he.Message)
					if entry.StatusCode == 0 {
						entry.StatusCode = he.Code
					}
				} else {
					entry.Error = err.Error()
				}
			}

			if shouldLogRequest(path, entry) {
				go service.LogRequest(entry)
				go service.RotateIfNeeded()
			}

			return err
		}
	}
}

func shouldLogRequest(path string, entry *RequestLogEntry) bool {
	if path == "/health" || path == "/api/health" || path == "/ws/agent/status" {
		return false
	}

	if entry.AuthStatus == AuthStatusFailed || entry.Error != "" {
		return true
	}

	return true
}

func extractPathParams(c echo.Context, entry *RequestLogEntry) {
	if stackName := c.Param("name"); stackName != "" {
		entry.StackName = stackName
	} else if stackName := c.Param("stackName"); stackName != "" {
		entry.StackName = stackName
	}

	if containerName := c.Param("containerName"); containerName != "" {
		entry.ContainerName = containerName
	}

	if operationID := c.Param("operationId"); operationID != "" {
		entry.OperationID = operationID
	}

	if path := c.QueryParam("path"); path != "" {
		entry.FilePath = path
	}

	if serviceName := c.QueryParam("service_name"); serviceName != "" {
		entry.Metadata["service_name"] = serviceName
	}

	if follow := c.QueryParam("follow"); follow != "" {
		entry.Metadata["follow"] = follow
	}

	if command, ok := c.Get("operation_command").(string); ok && command != "" {
		entry.Operation = command
	}

	if services, ok := c.Get("operation_services").([]string); ok && len(services) > 0 {
		entry.Metadata["services"] = strings.Join(services, ",")
	}

	if options, ok := c.Get("operation_options").([]string); ok && len(options) > 0 {
		entry.Metadata["options"] = strings.Join(options, ",")
	}

	if terminalStack, ok := c.Get("terminal_stack_name").(string); ok && terminalStack != "" {
		entry.StackName = terminalStack
		entry.Metadata["action"] = "terminal_session"
	}

	if terminalService, ok := c.Get("terminal_service_name").(string); ok && terminalService != "" {
		entry.Metadata["service_name"] = terminalService
	}

	if terminalContainer, ok := c.Get("terminal_container_name").(string); ok && terminalContainer != "" {
		entry.ContainerName = terminalContainer
	}
}

func HashToken(token string) string {
	if token == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(token))
	return fmt.Sprintf("sha256:%x", hash[:8])
}

func SetAuthSuccess(c echo.Context, token string) {
	c.Set(AuthStatusContextKey, AuthStatusSuccess)
	if token != "" {
		c.Set(AuthTokenHashContextKey, HashToken(token))
	}
}

func SetAuthFailure(c echo.Context, reason string) {
	c.Set(AuthStatusContextKey, AuthStatusFailed)
	c.Set(AuthErrorContextKey, reason)
}

func SetAuthSkipped(c echo.Context) {
	c.Set(AuthStatusContextKey, AuthStatusSkipped)
}

func ExtractBearerToken(authHeader string) string {
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
		return parts[1]
	}
	return ""
}
