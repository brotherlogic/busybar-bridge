package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/calendar"
	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
	"github.com/brotherlogic/busybar-bridge/pkg/pb"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Port != 8080 {
		t.Errorf("expected default Port 8080, got %d", cfg.Port)
	}
	if cfg.ReadTimeout != 5*time.Second {
		t.Errorf("expected default ReadTimeout 5s, got %v", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 10*time.Second {
		t.Errorf("expected default WriteTimeout 10s, got %v", cfg.WriteTimeout)
	}
}

func TestNew_Defaults(t *testing.T) {
	store := telemetry.NewStore()
	srv := New(Config{}, store)

	if srv.Config().Port != 8080 {
		t.Errorf("expected default Port 8080, got %d", srv.Config().Port)
	}
	if srv.Config().ReadTimeout != 5*time.Second {
		t.Errorf("expected default ReadTimeout 5s, got %v", srv.Config().ReadTimeout)
	}
	if srv.Config().WriteTimeout != 10*time.Second {
		t.Errorf("expected default WriteTimeout 10s, got %v", srv.Config().WriteTimeout)
	}
	if srv.Store() != store {
		t.Errorf("expected store to match provided telemetry store")
	}
	if srv.HTTPServer() == nil {
		t.Fatalf("expected non-nil HTTPServer")
	}
	if srv.Handler() == nil {
		t.Fatalf("expected non-nil Handler")
	}
}

func TestHealthzEndpoint(t *testing.T) {
	store := telemetry.NewStore()
	srv := New(DefaultConfig(), store)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	expectedBody := "ok\n"
	if rec.Body.String() != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, rec.Body.String())
	}
}

func TestHealthzEndpoint_MethodNotAllowed(t *testing.T) {
	store := telemetry.NewStore()
	srv := New(DefaultConfig(), store)

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestReadyEndpointConnected(t *testing.T) {
	store := telemetry.NewStore()
	store.SetConnected(true)
	srv := New(DefaultConfig(), store)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	expectedBody := "ready\n"
	if rec.Body.String() != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, rec.Body.String())
	}
}

func TestReadyEndpointDisconnected(t *testing.T) {
	store := telemetry.NewStore()
	store.SetConnected(false)
	srv := New(DefaultConfig(), store)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
	expectedBody := "not ready\n"
	if rec.Body.String() != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, rec.Body.String())
	}
}

func TestReadyEndpoint_NilStore(t *testing.T) {
	srv := New(DefaultConfig(), nil)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
	expectedBody := "not ready\n"
	if rec.Body.String() != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, rec.Body.String())
	}
}

func TestReadyEndpoint_MethodNotAllowed(t *testing.T) {
	store := telemetry.NewStore()
	srv := New(DefaultConfig(), store)

	req := httptest.NewRequest(http.MethodPost, "/ready", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestGracefulShutdown(t *testing.T) {
	store := telemetry.NewStore()
	store.SetConnected(true)
	srv := New(Config{Port: -1}, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	addr := srv.Addr()
	if addr == "" {
		t.Fatalf("expected non-empty server address")
	}

	// Verify server is responding
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("failed to query /healthz on active server: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "ok\n" {
		t.Errorf("unexpected healthz response: code=%d, body=%q", resp.StatusCode, string(body))
	}

	// Shutdown server gracefully
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}

	// Verify requests fail after shutdown
	_, err = client.Get("http://" + addr + "/healthz")
	if err == nil {
		t.Errorf("expected connection error after shutdown, got nil")
	}
}

func TestStatusPageRendering(t *testing.T) {
	store := telemetry.NewStore()
	store.SetConnected(true)
	id1 := store.RecordEvent("button", "Living Room Switch toggled ON")
	store.RecordForwardOutcome(id1, telemetry.OutcomeSuccess, 45*time.Millisecond, nil)
	id2 := store.RecordEvent("encoder", "Dial rotated 3 steps clockwise")
	store.RecordForwardOutcome(id2, telemetry.OutcomeFailure, 120*time.Millisecond, errors.New("timeout connecting to HA"))

	srv := New(DefaultConfig(), store)

	for _, path := range []string{"/status", "/"} {
		t.Run("path_"+path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
			}
			contentType := rec.Header().Get("Content-Type")
			if !strings.Contains(contentType, "text/html") {
				t.Errorf("expected Content-Type containing text/html, got %q", contentType)
			}

			body := rec.Body.String()
			if !strings.Contains(body, `<meta http-equiv="refresh" content="5">`) {
				t.Errorf("expected meta-refresh tag in body")
			}
			if !strings.Contains(body, "Connected") {
				t.Errorf("expected Connected badge in body")
			}
			if !strings.Contains(body, "Living Room Switch toggled ON") {
				t.Errorf("expected event 1 summary in body")
			}
			if !strings.Contains(body, "Dial rotated 3 steps clockwise") {
				t.Errorf("expected event 2 summary in body")
			}
			if !strings.Contains(body, "timeout connecting to HA") {
				t.Errorf("expected error message in body")
			}
		})
	}
}

func TestStatusPageEmptyState(t *testing.T) {
	store := telemetry.NewStore()
	store.SetConnected(false)
	srv := New(DefaultConfig(), store)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected Content-Type containing text/html, got %q", contentType)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "No events received yet") {
		t.Errorf("expected empty state message 'No events received yet' in body")
	}
	if !strings.Contains(body, "Disconnected") {
		t.Errorf("expected Disconnected badge in body")
	}
}

func TestStatusPage_MethodNotAllowed(t *testing.T) {
	store := telemetry.NewStore()
	srv := New(DefaultConfig(), store)

	for _, path := range []string{"/status", "/"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("path %s: expected status %d, got %d", path, http.StatusMethodNotAllowed, rec.Code)
		}
	}
}

func TestStatusPage_NotFound(t *testing.T) {
	store := telemetry.NewStore()
	srv := New(DefaultConfig(), store)

	req := httptest.NewRequest(http.MethodGet, "/unknown-page", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestStatusPage_NilStore(t *testing.T) {
	srv := New(DefaultConfig(), nil)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestStatusPage_TemplateError(t *testing.T) {
	store := telemetry.NewStore()
	srv := New(DefaultConfig(), store)
	// Override tmpl to nil or a template that errors out
	srv.tmpl = nil

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

type mockCalendarStore struct {
	mu      sync.RWMutex
	bound   bool
	binding *pb.CalendarBinding
	saveErr error
}

func (m *mockCalendarStore) IsBound() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bound
}

func (m *mockCalendarStore) Get() (*pb.CalendarBinding, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.binding, m.bound
}

func (m *mockCalendarStore) Save(binding *pb.CalendarBinding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	if m.bound {
		return calendar.ErrAlreadyBound
	}
	m.bound = true
	m.binding = binding
	return nil
}

type mockOAuthManager struct {
	authURL     string
	state       string
	authErr     error
	binding     *pb.CalendarBinding
	exchangeErr error
	configured  bool
}

func (m *mockOAuthManager) GenerateAuthURL() (string, string, error) {
	if m.authErr != nil {
		return "", "", m.authErr
	}
	return m.authURL, m.state, nil
}

func (m *mockOAuthManager) ExchangeCode(ctx context.Context, code string) (*pb.CalendarBinding, error) {
	if m.exchangeErr != nil {
		return nil, m.exchangeErr
	}
	return m.binding, nil
}

func (m *mockOAuthManager) IsConfigured() bool {
	return m.configured
}

func TestOAuthLogin_UnconfiguredRedirect(t *testing.T) {
	store := telemetry.NewStore()
	srv := New(DefaultConfig(), store)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/login", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect status, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/status?error=oauth_unconfigured" {
		t.Errorf("expected redirect to /status?error=oauth_unconfigured, got %q", loc)
	}
}

func TestOAuthLogin_AlreadyBoundConflict(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{bound: true}
	oauthMgr := &mockOAuthManager{configured: true}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/login", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d", http.StatusConflict, rec.Code)
	}
}

func TestOAuthLogin_Success(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{bound: false}
	oauthMgr := &mockOAuthManager{
		configured: true,
		authURL:    "https://accounts.google.com/o/oauth2/v2/auth?state=csrf-token-123",
		state:      "csrf-token-123",
	}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/login", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect && rec.Code != http.StatusFound && rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect status, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != oauthMgr.authURL {
		t.Errorf("expected redirect to %q, got %q", oauthMgr.authURL, loc)
	}

	cookies := rec.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "oauth_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatalf("expected oauth_state cookie to be set")
	}
	if stateCookie.Value != "csrf-token-123" {
		t.Errorf("expected cookie value 'csrf-token-123', got %q", stateCookie.Value)
	}
	if !stateCookie.HttpOnly {
		t.Errorf("expected HttpOnly cookie")
	}
	if stateCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", stateCookie.SameSite)
	}
	if stateCookie.Path != "/" {
		t.Errorf("expected Path=/, got %q", stateCookie.Path)
	}
	if stateCookie.MaxAge != 600 {
		t.Errorf("expected MaxAge=600 (10 mins), got %d", stateCookie.MaxAge)
	}
}

func TestOAuthCallback_AlreadyBoundConflict(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{bound: true}
	oauthMgr := &mockOAuthManager{configured: true}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/callback?code=abc&state=xyz", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "xyz"})
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d", http.StatusConflict, rec.Code)
	}
}

func TestOAuthCallback_StateVerification(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{bound: false}
	oauthMgr := &mockOAuthManager{configured: true}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	tests := []struct {
		name         string
		cookieState  string
		queryState   string
		expectedCode int
	}{
		{
			name:         "missing cookie",
			cookieState:  "",
			queryState:   "valid-state",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "missing query state",
			cookieState:  "valid-state",
			queryState:   "",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "mismatched state",
			cookieState:  "state-one",
			queryState:   "state-two",
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := "/oauth/google/callback?code=abc"
			if tc.queryState != "" {
				path += "&state=" + tc.queryState
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if tc.cookieState != "" {
				req.AddCookie(&http.Cookie{Name: "oauth_state", Value: tc.cookieState})
			}
			rec := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != tc.expectedCode {
				t.Errorf("expected code %d, got %d", tc.expectedCode, rec.Code)
			}
		})
	}
}

func TestOAuthCallback_OAuthErrorRedirect(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{bound: false}
	oauthMgr := &mockOAuthManager{configured: true}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/callback?error=access_denied&state=secret-state", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "secret-state"})
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusTemporaryRedirect && rec.Code != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/status?error=access_denied" {
		t.Errorf("expected redirect to /status?error=access_denied, got %q", loc)
	}

	cookies := rec.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "oauth_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie != nil && stateCookie.MaxAge > 0 {
		t.Errorf("expected oauth_state cookie to be cleared (MaxAge <= 0), got %d", stateCookie.MaxAge)
	}
}

func TestOAuthCallback_ExchangeSuccess(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{bound: false}
	expectedBinding := &pb.CalendarBinding{
		Email:        "user@example.com",
		AccessToken:  "token-123",
		RefreshToken: "refresh-123",
		LinkedAtUnix: time.Now().Unix(),
	}
	oauthMgr := &mockOAuthManager{
		configured: true,
		binding:    expectedBinding,
	}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/callback?code=auth-code-xyz&state=my-csrf-state", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "my-csrf-state"})
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusTemporaryRedirect && rec.Code != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/status?success=linked" {
		t.Errorf("expected redirect to /status?success=linked, got %q", loc)
	}

	if !calStore.IsBound() {
		t.Errorf("expected calendar store to be bound after successful exchange")
	}
	b, ok := calStore.Get()
	if !ok || b == nil || b.Email != "user@example.com" {
		t.Errorf("expected saved binding email to be user@example.com, got %v", b)
	}

	cookies := rec.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "oauth_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie != nil && stateCookie.MaxAge > 0 {
		t.Errorf("expected oauth_state cookie to be cleared, got %d", stateCookie.MaxAge)
	}
}

func TestStatusPage_CalendarStatusData(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{
		bound: true,
		binding: &pb.CalendarBinding{
			Email:        "admin@example.com",
			LinkedAtUnix: 1774000000,
		},
	}
	oauthMgr := &mockOAuthManager{configured: true}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/status?error=foo_err&success=bar_succ", nil)
	status := srv.CalendarStatus(req)

	if !status.Configured {
		t.Errorf("expected Configured=true")
	}
	if !status.Bound {
		t.Errorf("expected Bound=true")
	}
	if status.Email != "admin@example.com" {
		t.Errorf("expected Email='admin@example.com', got %q", status.Email)
	}
	if status.LinkedAt == "" {
		t.Errorf("expected LinkedAt to be non-empty")
	}
	if status.FlashError != "foo_err" {
		t.Errorf("expected FlashError='foo_err', got %q", status.FlashError)
	}
	if status.FlashSuccess != "bar_succ" {
		t.Errorf("expected FlashSuccess='bar_succ', got %q", status.FlashSuccess)
	}
}

func TestOAuthLogin_MethodNotAllowed(t *testing.T) {
	store := telemetry.NewStore()
	srv := New(DefaultConfig(), store)

	req := httptest.NewRequest(http.MethodPost, "/oauth/google/login", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestOAuthCallback_MethodNotAllowed(t *testing.T) {
	store := telemetry.NewStore()
	srv := New(DefaultConfig(), store)

	req := httptest.NewRequest(http.MethodPost, "/oauth/google/callback", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestOAuthLogin_GenerateURLError(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{bound: false}
	oauthMgr := &mockOAuthManager{
		configured: true,
		authErr:    errors.New("crypto entropy failure"),
	}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/login", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestOAuthCallback_MissingCode(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{bound: false}
	oauthMgr := &mockOAuthManager{configured: true}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/callback?state=my-state", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "my-state"})
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestOAuthCallback_ExchangeFailure(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{bound: false}
	oauthMgr := &mockOAuthManager{
		configured:  true,
		exchangeErr: errors.New("upstream connection reset"),
	}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/callback?code=bad-code&state=my-state", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "my-state"})
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusTemporaryRedirect && rec.Code != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/status?error=exchange_failed" {
		t.Errorf("expected redirect to /status?error=exchange_failed, got %q", loc)
	}
}

func TestOAuthCallback_SaveGeneralFailure(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{
		bound:   false,
		saveErr: errors.New("disk full"),
	}
	oauthMgr := &mockOAuthManager{
		configured: true,
		binding:    &pb.CalendarBinding{Email: "user@example.com"},
	}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/callback?code=ok-code&state=my-state", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "my-state"})
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusTemporaryRedirect && rec.Code != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/status?error=save_failed" {
		t.Errorf("expected redirect to /status?error=save_failed, got %q", loc)
	}
}

func TestOAuthCallback_SaveConflict(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{
		bound:   false,
		saveErr: calendar.ErrAlreadyBound,
	}
	oauthMgr := &mockOAuthManager{
		configured: true,
		binding:    &pb.CalendarBinding{Email: "user@example.com"},
	}
	srv := New(DefaultConfig(), store, WithCalendarStore(calStore), WithOAuthManager(oauthMgr))

	req := httptest.NewRequest(http.MethodGet, "/oauth/google/callback?code=ok-code&state=my-state", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "my-state"})
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d", http.StatusConflict, rec.Code)
	}
}

func TestServer_OptionsAndGetters(t *testing.T) {
	store := telemetry.NewStore()
	calStore := &mockCalendarStore{bound: false}
	oauthMgr := &mockOAuthManager{configured: true}

	opts := ServerOptions{
		CalendarStore:   calStore,
		CalendarManager: oauthMgr,
	}
	srv := NewWithOptions(DefaultConfig(), store, opts)

	if srv.CalendarStore() != calStore {
		t.Errorf("expected CalendarStore to match")
	}
	if srv.OAuthManager() != oauthMgr {
		t.Errorf("expected OAuthManager to match")
	}
	if srv.CalendarManager() != oauthMgr {
		t.Errorf("expected CalendarManager to match")
	}

	// Test functional options
	srv2 := New(DefaultConfig(), store,
		WithCalendarStore(calStore),
		WithCalendarManager(oauthMgr),
		WithOAuthConfigured(true),
	)
	if srv2.CalendarStore() != calStore {
		t.Errorf("expected CalendarStore to match")
	}
	if srv2.OAuthManager() != oauthMgr {
		t.Errorf("expected OAuthManager to match")
	}
}




