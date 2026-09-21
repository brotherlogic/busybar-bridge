package server

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/calendar"
	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
	"github.com/brotherlogic/busybar-bridge/pkg/pb"
)

//go:embed templates/status.html
var statusHTML string

// CalendarStore defines the persistence interface for calendar bindings required by the server.
type CalendarStore interface {
	IsBound() bool
	Get() (*pb.CalendarBinding, bool)
	Save(binding *pb.CalendarBinding) error
}

// OAuthManager defines the OAuth authorization and code exchange interface required by the server.
type OAuthManager interface {
	GenerateAuthURL() (string, string, error)
	ExchangeCode(ctx context.Context, code string) (*pb.CalendarBinding, error)
}

// CalendarManager is an alias for OAuthManager to support flexible nomenclature.
type CalendarManager = OAuthManager

// CalendarStatus holds Google Calendar connection and authorization status for dashboard rendering.
type CalendarStatus struct {
	Configured   bool
	Bound        bool
	Email        string
	LinkedAt     string
	FlashError   string
	FlashSuccess string
}

// StatusData encapsulates the telemetry snapshot and calendar status for dashboard rendering.
type StatusData struct {
	telemetry.Snapshot
	Calendar CalendarStatus
}

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

// ServerOptions defines optional dependencies and settings for Server initialization.
type ServerOptions struct {
	CalendarStore   CalendarStore
	OAuthManager    OAuthManager
	CalendarManager OAuthManager
	OAuthConfigured *bool
}

// ServerOption represents a functional option for configuring a Server.
type ServerOption func(*Server)

// WithServerOptions applies a ServerOptions struct configuration to the Server.
func WithServerOptions(opts ServerOptions) ServerOption {
	return func(s *Server) {
		if opts.CalendarStore != nil {
			s.calendarStore = opts.CalendarStore
		}
		if opts.OAuthManager != nil {
			s.oauthMgr = opts.OAuthManager
		} else if opts.CalendarManager != nil {
			s.oauthMgr = opts.CalendarManager
		}
		if opts.OAuthConfigured != nil {
			s.oauthConfigured = opts.OAuthConfigured
		}
	}
}

// WithCalendarStore configures the CalendarStore for the server.
func WithCalendarStore(store CalendarStore) ServerOption {
	return func(s *Server) {
		s.calendarStore = store
	}
}

// WithOAuthManager configures the OAuthManager for the server.
func WithOAuthManager(mgr OAuthManager) ServerOption {
	return func(s *Server) {
		s.oauthMgr = mgr
	}
}

// WithCalendarManager configures the CalendarManager (OAuthManager) for the server.
func WithCalendarManager(mgr OAuthManager) ServerOption {
	return WithOAuthManager(mgr)
}

// WithOAuthConfigured explicitly sets the OAuth configured status.
func WithOAuthConfigured(configured bool) ServerOption {
	return func(s *Server) {
		s.oauthConfigured = &configured
	}
}

// Server provides HTTP lifecycle management, observability probe endpoints,
// and Google Calendar OAuth linking endpoints.
type Server struct {
	cfg             Config
	store           *telemetry.Store
	calendarStore   CalendarStore
	oauthMgr        OAuthManager
	oauthConfigured *bool
	httpServer      *http.Server
	listener        net.Listener
	tmpl            *template.Template
	mu              sync.Mutex
}

// New initializes a Server configured with probe routes, OAuth endpoints, and options.
func New(cfg Config, store *telemetry.Store, opts ...ServerOption) *Server {
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

	tmpl, err := template.New("status.html").Parse(statusHTML)
	if err != nil {
		log.Printf("failed to parse status template: %v", err)
	}

	s := &Server{
		cfg:   cfg,
		store: store,
		tmpl:  tmpl,
	}

	for _, opt := range opts {
		opt(s)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/", s.handleStatus)
	mux.HandleFunc("/oauth/google/login", s.handleOAuthGoogleLogin)
	mux.HandleFunc("/oauth/google/callback", s.handleOAuthGoogleCallback)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s
}

// NewWithOptions constructs a new Server given Config, telemetry Store, and ServerOptions.
func NewWithOptions(cfg Config, store *telemetry.Store, opts ServerOptions) *Server {
	return New(cfg, store, WithServerOptions(opts))
}

// NewServer is an alias for New to construct a new Server instance.
func NewServer(cfg Config, store *telemetry.Store, opts ...ServerOption) *Server {
	return New(cfg, store, opts...)
}

// Config returns a copy of the server configuration.
func (s *Server) Config() Config {
	return s.cfg
}

// Store returns the underlying telemetry store.
func (s *Server) Store() *telemetry.Store {
	return s.store
}

// CalendarStore returns the configured CalendarStore dependency.
func (s *Server) CalendarStore() CalendarStore {
	return s.calendarStore
}

// OAuthManager returns the configured OAuthManager dependency.
func (s *Server) OAuthManager() OAuthManager {
	return s.oauthMgr
}

// CalendarManager returns the configured OAuthManager dependency.
func (s *Server) CalendarManager() OAuthManager {
	return s.oauthMgr
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

func (s *Server) isOAuthConfigured() bool {
	if s.oauthConfigured != nil {
		return *s.oauthConfigured
	}
	if s.oauthMgr == nil {
		return false
	}
	if checker, ok := s.oauthMgr.(interface{ IsConfigured() bool }); ok {
		return checker.IsConfigured()
	}
	return true
}

// CalendarStatus builds and returns the current CalendarStatus context based on server dependencies and request query.
func (s *Server) CalendarStatus(r *http.Request) CalendarStatus {
	return s.calendarStatus(r)
}

func (s *Server) calendarStatus(r *http.Request) CalendarStatus {
	status := CalendarStatus{
		Configured: s.isOAuthConfigured(),
	}

	if r != nil {
		status.FlashError = r.URL.Query().Get("error")
		status.FlashSuccess = r.URL.Query().Get("success")
	}

	if s.calendarStore != nil {
		status.Bound = s.calendarStore.IsBound()
		if binding, ok := s.calendarStore.Get(); ok && binding != nil {
			status.Email = binding.GetEmail()
			if binding.GetLinkedAtUnix() > 0 {
				status.LinkedAt = time.Unix(binding.GetLinkedAtUnix(), 0).UTC().Format(time.RFC3339)
			}
		}
	}

	return status
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

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/status" && r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var snapshot telemetry.Snapshot
	if s.store != nil {
		snapshot = s.store.Snapshot()
	}

	data := StatusData{
		Snapshot: snapshot,
		Calendar: s.calendarStatus(r),
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") || r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("failed to encode status json: %v", err)
		}
		return
	}

	if s.tmpl == nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = buf.WriteTo(w)
}

func (s *Server) handleOAuthGoogleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// If calendar is already bound, return 409 Conflict
	if s.calendarStore != nil && s.calendarStore.IsBound() {
		http.Error(w, "conflict: calendar already bound", http.StatusConflict)
		return
	}

	// If OAuth credentials are unconfigured, redirect to /status?error=oauth_unconfigured
	if !s.isOAuthConfigured() {
		http.Redirect(w, r, "/status?error=oauth_unconfigured", http.StatusSeeOther)
		return
	}

	// Generate CSRF state and auth URL via calendar manager
	authURL, state, err := s.oauthMgr.GenerateAuthURL()
	if err != nil {
		log.Printf("failed to generate OAuth auth URL: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Set oauth_state HttpOnly cookie (10-minute TTL, SameSite=Lax, Path=/)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		Expires:  time.Now().Add(10 * time.Minute),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect client to Google authorization URL
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func (s *Server) handleOAuthGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// If calendar is already bound, return 409 Conflict
	if s.calendarStore != nil && s.calendarStore.IsBound() {
		http.Error(w, "conflict: calendar already bound", http.StatusConflict)
		return
	}

	// Validate that state query parameter matches oauth_state cookie; if missing or mismatched, return 400 Bad Request
	cookie, err := r.Cookie("oauth_state")
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		http.Error(w, "bad request: missing oauth_state cookie", http.StatusBadRequest)
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" || state != cookie.Value {
		http.Error(w, "bad request: invalid or mismatched state parameter", http.StatusBadRequest)
		return
	}

	// Check for Google OAuth query error (e.g. error=access_denied) and redirect to /status?error=access_denied
	if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
		s.clearStateCookie(w)
		http.Redirect(w, r, "/status?error="+url.QueryEscape(oauthErr), http.StatusSeeOther)
		return
	}

	code := r.URL.Query().Get("code")
	if strings.TrimSpace(code) == "" {
		http.Error(w, "bad request: missing authorization code", http.StatusBadRequest)
		return
	}

	if s.oauthMgr == nil {
		s.clearStateCookie(w)
		http.Redirect(w, r, "/status?error=oauth_unconfigured", http.StatusSeeOther)
		return
	}

	// Exchange authorization code via calendar.Manager.ExchangeCode(ctx, code)
	binding, err := s.oauthMgr.ExchangeCode(r.Context(), code)
	if err != nil {
		log.Printf("failed to exchange authorization code: %v", err)
		s.clearStateCookie(w)
		http.Redirect(w, r, "/status?error=exchange_failed", http.StatusSeeOther)
		return
	}

	// Save resulting binding via calendar.Store.Save(binding)
	if s.calendarStore != nil {
		if err := s.calendarStore.Save(binding); err != nil {
			log.Printf("failed to save calendar binding: %v", err)
			s.clearStateCookie(w)
			if errors.Is(err, calendar.ErrAlreadyBound) {
				http.Error(w, "conflict: calendar already bound", http.StatusConflict)
				return
			}
			http.Redirect(w, r, "/status?error=save_failed", http.StatusSeeOther)
			return
		}
	}

	// Clear oauth_state cookie
	s.clearStateCookie(w)

	// Redirect to /status?success=linked
	http.Redirect(w, r, "/status?success=linked", http.StatusSeeOther)
}

func (s *Server) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
