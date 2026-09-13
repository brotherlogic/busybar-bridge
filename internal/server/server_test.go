package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
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


