package agentsign

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	HeaderSignature   = "X-Berth-Signature"
	HeaderCertificate = "X-Berth-Certificate"
	HeaderTimestamp   = "X-Berth-Timestamp"
	HeaderNonce       = "X-Berth-Nonce"

	RequestContext = "berth-request-v1"
	ServerIdentity = "berth-server"
)

func Canonical(fields ...string) []byte {
	var base bytes.Buffer
	for _, field := range fields {
		_ = binary.Write(&base, binary.BigEndian, uint32(len(field)))
		base.WriteString(field)
	}
	return base.Bytes()
}

func RequestBase(method, target, contentType string, body []byte, timestamp int64, nonce string) []byte {
	digest := sha256.Sum256(body)
	return Canonical(
		RequestContext,
		method,
		target,
		contentType,
		hex.EncodeToString(digest[:]),
		strconv.FormatInt(timestamp, 10),
		nonce,
	)
}

var ErrRejected = errors.New("request signature rejected")

type nonceCache struct {
	mutex  sync.Mutex
	seen   map[string]time.Time
	retain time.Duration
	swept  time.Time
}

func newNonceCache(retain time.Duration) *nonceCache {
	return &nonceCache{seen: map[string]time.Time{}, retain: retain, swept: time.Now()}
}

func (c *nonceCache) admit(nonce string, now time.Time) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if now.Sub(c.swept) > c.retain {
		for value, seenAt := range c.seen {
			if now.Sub(seenAt) > c.retain {
				delete(c.seen, value)
			}
		}
		c.swept = now
	}

	if _, replayed := c.seen[nonce]; replayed {
		return false
	}
	c.seen[nonce] = now
	return true
}

type Verifier struct {
	authority *x509.CertPool
	skew      time.Duration
	nonces    *nonceCache
}

func NewVerifier(authorityPEM []byte, skew time.Duration) (*Verifier, error) {
	authority := x509.NewCertPool()
	if !authority.AppendCertsFromPEM(authorityPEM) {
		return nil, errors.New("the berth certificate authority file does not contain a certificate")
	}
	return &Verifier{
		authority: authority,
		skew:      skew,
		nonces:    newNonceCache(2 * skew),
	}, nil
}

func (v *Verifier) VerifyRequest(req *http.Request, body []byte) error {
	signature, err := base64.StdEncoding.DecodeString(req.Header.Get(HeaderSignature))
	if err != nil || len(signature) == 0 {
		return ErrRejected
	}
	certificateDER, err := base64.StdEncoding.DecodeString(req.Header.Get(HeaderCertificate))
	if err != nil || len(certificateDER) == 0 {
		return ErrRejected
	}
	nonce := req.Header.Get(HeaderNonce)
	if nonce == "" {
		return ErrRejected
	}
	timestamp, err := strconv.ParseInt(req.Header.Get(HeaderTimestamp), 10, 64)
	if err != nil {
		return ErrRejected
	}

	now := time.Now()
	if difference := now.Sub(time.Unix(timestamp, 0)); difference > v.skew || difference < -v.skew {
		return ErrRejected
	}

	certificate, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		return ErrRejected
	}
	if _, err := certificate.Verify(x509.VerifyOptions{
		Roots:     v.authority,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return ErrRejected
	}
	if err := certificate.VerifyHostname(ServerIdentity); err != nil {
		return ErrRejected
	}

	publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return ErrRejected
	}

	base := RequestBase(req.Method, req.URL.RequestURI(), req.Header.Get("Content-Type"), body, timestamp, nonce)
	digest := sha256.Sum256(base)
	if !ecdsa.VerifyASN1(publicKey, digest[:], signature) {
		return ErrRejected
	}

	if !v.nonces.admit(nonce, now) {
		return ErrRejected
	}
	return nil
}

func ReadBody(req *http.Request, limit int64) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	body, err := readLimited(req, limit)
	if err != nil {
		return nil, err
	}
	req.Body = newReplayBody(body)
	return body, nil
}

func readLimited(req *http.Request, limit int64) ([]byte, error) {
	var collected bytes.Buffer
	written, err := collected.ReadFrom(&limitedReader{reader: req.Body, remaining: limit + 1})
	if err != nil {
		return nil, err
	}
	if written > limit {
		return nil, fmt.Errorf("request body exceeds the %d byte signing limit", limit)
	}
	return collected.Bytes(), nil
}
