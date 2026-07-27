package agentsign

import (
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

const (
	AuthorityFileName   = "ca.crt"
	CertificateFileName = "agent.crt"
	KeyFileName         = "agent.key"
	DefaultSkew         = time.Minute
)

func LoadMaterial(certDir string) (*Verifier, *Responder, error) {
	authorityPath := filepath.Join(certDir, AuthorityFileName)
	authorityPEM, err := os.ReadFile(authorityPath)
	if err != nil {
		return nil, nil, &startupError{path: authorityPath, cause: err}
	}
	verifier, err := NewVerifier(authorityPEM, DefaultSkew)
	if err != nil {
		return nil, nil, err
	}

	certificatePath := filepath.Join(certDir, CertificateFileName)
	certPEM, err := os.ReadFile(certificatePath)
	if err != nil {
		return nil, nil, &startupError{path: certificatePath, cause: err}
	}
	keyPEM, err := os.ReadFile(filepath.Join(certDir, KeyFileName))
	if err != nil {
		return nil, nil, &startupError{path: filepath.Join(certDir, KeyFileName), cause: err}
	}
	responder, err := NewResponder(certPEM, keyPEM)
	if err != nil {
		return nil, nil, err
	}

	return verifier, responder, nil
}

type startupError struct {
	path  string
	cause error
}

func (e *startupError) Error() string {
	return "could not read " + e.path + ": " + e.cause.Error() +
		"; issue an agent certificate bundle for this server in berth and place agent.crt, agent.key and ca.crt in " + filepath.Dir(e.path)
}

func (e *startupError) Unwrap() error { return e.cause }

type streamContext struct {
	peer      *x509.Certificate
	nonce     string
	responder *Responder
}

const streamContextKey = "berth.stream"

func sessionKey(c echo.Context) ([]byte, error) {
	stored, ok := c.Get(streamContextKey).(streamContext)
	if !ok {
		return nil, errors.New("this request was not verified, so no session key exists")
	}
	return stored.responder.SessionKeyFor(stored.peer, stored.nonce)
}

func SessionFor(c echo.Context, direction string) (*FrameWriter, *FrameReader, error) {
	key, err := sessionKey(c)
	if err != nil {
		return nil, nil, err
	}
	opposite := DirectionToBerth
	if direction == DirectionToBerth {
		opposite = DirectionToAgent
	}
	return NewFrameWriter(key, direction), NewFrameReader(key, opposite), nil
}

func Middleware(verifier *Verifier, responder *Responder, maxBodyBytes int64, logger *logging.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			request := c.Request()

			peer, err := verifier.VerifyRequest(request)
			if err != nil {
				logger.Warn("rejected a request that berth did not sign",
					zap.String("source_ip", c.RealIP()),
					zap.String("method", request.Method),
					zap.String("path", request.URL.Path),
				)
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid request signature"})
			}

			body, err := ReadBody(request, maxBodyBytes)
			if err != nil {
				logger.Warn("rejected an oversized request body",
					zap.String("source_ip", c.RealIP()),
					zap.String("path", request.URL.Path),
				)
				return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "Request too large"})
			}

			if err := VerifyBody(request, body); err != nil {
				logger.Warn("rejected a request whose body is not the one berth signed",
					zap.String("source_ip", c.RealIP()),
					zap.String("method", request.Method),
					zap.String("path", request.URL.Path),
				)
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid request signature"})
			}

			c.Set(streamContextKey, streamContext{
				peer:      peer,
				nonce:     request.Header.Get(HeaderNonce),
				responder: responder,
			})

			return next(c)
		}
	}
}
