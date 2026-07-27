package agentsign

import (
	"bufio"
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
)

type Responder struct {
	certificate []byte
	key         crypto.Signer
}

func NewResponder(certPEM, keyPEM []byte) (*Responder, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, errors.New("the agent certificate is not valid PEM")
	}
	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("the agent certificate could not be parsed: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("the agent key is not valid PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("the agent key could not be parsed: %w", err)
	}
	key, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, errors.New("the agent key cannot sign")
	}

	return &Responder{certificate: certificate.Raw, key: key}, nil
}

func (r *Responder) sign(header http.Header, requestNonce string, status int, contentType, bodyDigest string) error {
	timestamp := time.Now().Unix()
	base := ResponseBase(requestNonce, status, contentType, bodyDigest, timestamp)
	digest := sha256.Sum256(base)
	signature, err := r.key.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return err
	}

	header.Set(HeaderSignature, base64.StdEncoding.EncodeToString(signature))
	header.Set(HeaderCertificate, base64.StdEncoding.EncodeToString(r.certificate))
	header.Set(HeaderTimestamp, strconv.FormatInt(timestamp, 10))
	header.Set(HeaderBodyDigest, bodyDigest)
	return nil
}

func (r *Responder) SessionKeyFor(peer *x509.Certificate, salt string) ([]byte, error) {
	local, ok := r.key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("the agent key cannot agree a session key")
	}
	remote, ok := peer.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("berth's certificate cannot agree a session key")
	}
	return SessionKey(local, remote, salt)
}

func SignResponses(responder *Responder, limit int64) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			writer := &signingWriter{
				ResponseWriter: c.Response().Writer,
				responder:      responder,
				requestNonce:   c.Request().Header.Get(HeaderNonce),
				limit:          limit,
				status:         http.StatusOK,
			}
			c.Response().Writer = writer

			err := next(c)
			writer.finish()
			return err
		}
	}
}

func SignUpgradeResponses(responder *Responder) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if err := responder.sign(
				c.Response().Header(),
				c.Request().Header.Get(HeaderNonce),
				http.StatusSwitchingProtocols,
				"",
				BodyUnsigned,
			); err != nil {
				return err
			}
			return next(c)
		}
	}
}

type signingWriter struct {
	http.ResponseWriter
	responder    *Responder
	requestNonce string
	limit        int64
	status       int
	buffer       bytes.Buffer
	committed    bool
}

func (w *signingWriter) WriteHeader(status int) {
	w.status = status
}

func (w *signingWriter) Write(payload []byte) (int, error) {
	if w.committed {
		return w.ResponseWriter.Write(payload)
	}
	if int64(w.buffer.Len()+len(payload)) > w.limit {
		w.commit(BodyUnsigned)
		return w.ResponseWriter.Write(payload)
	}
	return w.buffer.Write(payload)
}

func (w *signingWriter) Flush() {
	if !w.committed {
		w.commit(BodyUnsigned)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *signingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("the response writer cannot be hijacked")
	}
	if !w.committed {
		_ = w.responder.sign(w.Header(), w.requestNonce, http.StatusSwitchingProtocols, "", BodyUnsigned)
		w.committed = true
	}
	return hijacker.Hijack()
}

func (w *signingWriter) finish() {
	if w.committed {
		return
	}
	w.commit(BodyDigest(w.buffer.Bytes()))
}

func (w *signingWriter) commit(bodyDigest string) {
	_ = w.responder.sign(w.Header(), w.requestNonce, w.status, w.Header().Get("Content-Type"), bodyDigest)
	w.ResponseWriter.WriteHeader(w.status)
	if w.buffer.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.buffer.Bytes())
		w.buffer.Reset()
	}
	w.committed = true
}
