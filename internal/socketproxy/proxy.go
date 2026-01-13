package socketproxy

import (
	"berth-agent/internal/logging"
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"regexp"

	"go.uber.org/zap"
)

type Proxy struct {
	socketPath    string
	dockerSocket  string
	allowedRoutes []*allowedRoute
	logger        *logging.Logger
	server        *http.Server
	listener      net.Listener
}

type allowedRoute struct {
	method  string
	pattern *regexp.Regexp
}

func NewProxy(socketPath, dockerSocket string, logger *logging.Logger) *Proxy {
	return &Proxy{
		socketPath:   socketPath,
		dockerSocket: dockerSocket,
		logger:       logger,
		allowedRoutes: []*allowedRoute{
			{method: "GET", pattern: regexp.MustCompile(`^/v[\d.]+/images(/.*)?$`)},
			{method: "GET", pattern: regexp.MustCompile(`^/images(/.*)?$`)},
			{method: "GET", pattern: regexp.MustCompile(`^/v[\d.]+/distribution(/.*)?$`)},
			{method: "GET", pattern: regexp.MustCompile(`^/distribution(/.*)?$`)},
			{method: "GET", pattern: regexp.MustCompile(`^/v[\d.]+/version$`)},
			{method: "GET", pattern: regexp.MustCompile(`^/version$`)},
			{method: "GET", pattern: regexp.MustCompile(`^/v[\d.]+/_ping$`)},
			{method: "GET", pattern: regexp.MustCompile(`^/_ping$`)},
			{method: "HEAD", pattern: regexp.MustCompile(`^/v[\d.]+/_ping$`)},
			{method: "HEAD", pattern: regexp.MustCompile(`^/_ping$`)},
		},
	}
}

func (p *Proxy) isAllowed(method, path string) bool {
	for _, route := range p.allowedRoutes {
		if route.method == method && route.pattern.MatchString(path) {
			return true
		}
	}
	return false
}

func (p *Proxy) Start(ctx context.Context) error {
	dir := filepath.Dir(p.socketPath)
	p.logger.Info("creating socket directory", zap.String("dir", dir))
	if err := os.MkdirAll(dir, 0755); err != nil {
		p.logger.Error("failed to create socket directory", zap.Error(err))
		return err
	}

	p.logger.Info("removing existing socket", zap.String("path", p.socketPath))
	if err := os.RemoveAll(p.socketPath); err != nil && !os.IsNotExist(err) {
		p.logger.Error("failed to remove existing socket", zap.Error(err))
		return err
	}

	p.logger.Info("creating unix listener", zap.String("path", p.socketPath))
	listener, err := net.Listen("unix", p.socketPath)
	if err != nil {
		p.logger.Error("failed to create unix listener", zap.Error(err))
		return err
	}
	p.listener = listener

	p.logger.Info("setting socket permissions")
	if err := os.Chmod(p.socketPath, 0660); err != nil {
		p.logger.Error("failed to chmod socket", zap.Error(err))
		listener.Close()
		return err
	}

	if _, err := os.Stat(p.socketPath); err != nil {
		p.logger.Error("socket file does not exist after creation", zap.Error(err))
		return err
	}

	dockerTransport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", p.dockerSocket)
		},
	}

	reverseProxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "docker"
		},
		Transport: dockerTransport,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.isAllowed(r.Method, r.URL.Path) {
			p.logger.Warn("blocked docker API request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
			)
			http.Error(w, "Forbidden: this Docker API endpoint is not allowed", http.StatusForbidden)
			return
		}

		p.logger.Debug("proxying docker API request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
		)
		reverseProxy.ServeHTTP(w, r)
	})

	p.server = &http.Server{Handler: handler}

	p.logger.Info("docker socket proxy started",
		zap.String("listen", p.socketPath),
		zap.String("upstream", p.dockerSocket),
	)

	go func() {
		if err := p.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			p.logger.Error("socket proxy server error", zap.Error(err))
		}
	}()

	return nil
}

func (p *Proxy) Stop(ctx context.Context) error {
	if p.server != nil {
		if err := p.server.Shutdown(ctx); err != nil {
			return err
		}
	}
	if p.listener != nil {
		p.listener.Close()
	}
	os.RemoveAll(p.socketPath)
	p.logger.Info("docker socket proxy stopped")
	return nil
}
