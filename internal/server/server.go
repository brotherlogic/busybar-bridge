package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
)

// Config defines the HTTP server configuration parameters.
type Config struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// DefaultConfig returns sensible production defaults for the HTTP server.
func DefaultConfig() Config {
	return Config{
		Port:         8080,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
}

// Server provides HTTP lifecycle management and observability probe endpoints.
type Server struct {
	cfg        Config
	store      *telemetry.Store
	httpServer *http.Server
	listener   net.Listener
	mu         sync.Mutex
}

// New initializes a Server configured with probe routes and default settings.
func New(cfg Config, store *telemetry.Store) *Server {
	defaults := DefaultConfig()
	if cfg.Port == 0 {
		cfg.Port = defaults.Port
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = defaults.ReadTimeout
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = defaults.WriteTimeout
	}

	var addr string
	if cfg.Port < 0 {
		addr = ":0"
	} else {
		addr = fmt.Sprintf(":%d", cfg.Port)
	}

	s := &Server{
		cfg:   cfg,
		store: store,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/ready", s.handleReady)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s
}

// Config returns a copy of the server configuration.
func (s *Server) Config() Config {
	return s.cfg
}

// Store returns the underlying telemetry store.
func (s *Server) Store() *telemetry.Store {
	return s.store
}

// HTTPServer returns the internal http.Server instance.
func (s *Server) HTTPServer() *http.Server {
	return s.httpServer
}

// Handler returns the HTTP request handler mux.
func (s *Server) Handler() http.Handler {
	if s.httpServer != nil {
		return s.httpServer.Handler
	}
	return nil
}

// Addr returns the network address the server is listening on.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		if tcpAddr, ok := s.listener.Addr().(*net.TCPAddr); ok {
			return fmt.Sprintf("127.0.0.1:%d", tcpAddr.Port)
		}
		return s.listener.Addr().String()
	}
	if s.httpServer != nil {
		return s.httpServer.Addr
	}
	return ""
}

// Start starts the HTTP listener non-blocking and handles shutdown upon context cancellation.
func (s *Server) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	addr := s.httpServer.Addr
	if addr == "" {
		if s.cfg.Port < 0 {
			addr = ":0"
		} else {
			addr = fmt.Sprintf(":%d", s.cfg.Port)
		}
		s.httpServer.Addr = addr
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = ln

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server serve error: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdownCtx)
	}()

	return nil
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if s.store != nil && s.store.IsConnected() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("not ready\n"))
}
