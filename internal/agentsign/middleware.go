package agentsign

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

const (
	AuthorityFileName = "ca.crt"
	DefaultSkew       = time.Minute
)

func LoadVerifier(certDir string, maxBodyBytes int64) (*Verifier, int64, error) {
	authorityPath := filepath.Join(certDir, AuthorityFileName)
	authorityPEM, err := os.ReadFile(authorityPath)
	if err != nil {
		return nil, 0, &startupError{path: authorityPath, cause: err}
	}
	verifier, err := NewVerifier(authorityPEM, DefaultSkew)
	if err != nil {
		return nil, 0, err
	}
	return verifier, maxBodyBytes, nil
}

type startupError struct {
	path  string
	cause error
}

func (e *startupError) Error() string {
	return "could not read the berth certificate authority at " + e.path + ": " + e.cause.Error() +
		"; issue an agent certificate bundle for this server in berth and place ca.crt in " + filepath.Dir(e.path)
}

func (e *startupError) Unwrap() error { return e.cause }

func Middleware(verifier *Verifier, maxBodyBytes int64, logger *logging.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			request := c.Request()

			body, err := ReadBody(request, maxBodyBytes)
			if err != nil {
				logger.Warn("rejected an oversized request body before verifying its signature",
					zap.String("source_ip", c.RealIP()),
					zap.String("path", request.URL.Path),
				)
				return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "Request too large"})
			}

			if err := verifier.VerifyRequest(request, body); err != nil {
				logger.Warn("rejected a request that berth did not sign",
					zap.String("source_ip", c.RealIP()),
					zap.String("method", request.Method),
					zap.String("path", request.URL.Path),
				)
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid request signature"})
			}

			return next(c)
		}
	}
}
