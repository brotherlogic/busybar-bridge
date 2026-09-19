package calendar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGenerateAuthURL_EntropyAndFormat(t *testing.T) {
	cfg := ManagerConfig{
		ClientID:     "test-client-id.apps.googleusercontent.com",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/oauth/google/callback",
	}

	mgr := NewManager(cfg)

	// Test URL construction and query parameters
	authURL, state, err := mgr.GenerateAuthURL()
	if err != nil {
		t.Fatalf("GenerateAuthURL failed: %v", err)
	}

	if state == "" {
		t.Fatal("GenerateAuthURL returned empty state")
	}

	// Verify state is 32 random bytes base64 URL encoded
	decodedState, err := base64.URLEncoding.DecodeString(state)
	if err != nil {
		// Also try RawURLEncoding in case no padding was used
		decodedState, err = base64.RawURLEncoding.DecodeString(state)
		if err != nil {
			t.Fatalf("state is not valid base64 URL-encoded: %v", err)
		}
	}
	if len(decodedState) != 32 {
		t.Fatalf("expected 32 bytes of state entropy, got %d", len(decodedState))
	}

	parsedURL, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("failed to parse authURL %q: %v", authURL, err)
	}

	q := parsedURL.Query()
	if q.Get("client_id") != cfg.ClientID {
		t.Errorf("expected client_id %q, got %q", cfg.ClientID, q.Get("client_id"))
	}
	if q.Get("redirect_uri") != cfg.RedirectURL {
		t.Errorf("expected redirect_uri %q, got %q", cfg.RedirectURL, q.Get("redirect_uri"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("expected response_type=code, got %q", q.Get("response_type"))
	}
	if q.Get("access_type") != "offline" {
		t.Errorf("expected access_type=offline, got %q", q.Get("access_type"))
	}
	if q.Get("prompt") != "consent" {
		t.Errorf("expected prompt=consent, got %q", q.Get("prompt"))
	}
	if q.Get("state") != state {
		t.Errorf("expected state %q in query params, got %q", state, q.Get("state"))
	}

	scope := q.Get("scope")
	if !strings.Contains(scope, "https://www.googleapis.com/auth/calendar.events.readonly") {
		t.Errorf("expected calendar.events.readonly scope in %q", scope)
	}
	if !strings.Contains(scope, "https://www.googleapis.com/auth/userinfo.email") {
		t.Errorf("expected userinfo.email scope in %q", scope)
	}

	// Verify entropy across 100 consecutive generations (no collisions)
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		_, s, err := mgr.GenerateAuthURL()
		if err != nil {
			t.Fatalf("GenerateAuthURL iteration %d failed: %v", i, err)
		}
		if seen[s] {
			t.Fatalf("duplicate state generated at iteration %d: %s", i, s)
		}
		seen[s] = true
	}
}

func TestExchangeCode_Success(t *testing.T) {
	expectedEmail := "developer@example.com"
	expectedCode := "valid-auth-code-1234"
	expectedAccessToken := "ya29.test-access-token"
	expectedRefreshToken := "1//test-refresh-token"

	var tokenRequested, userInfoRequested int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			atomic.AddInt32(&tokenRequested, 1)
			if r.Method != http.MethodPost {
				http.Error(w, "expected POST", http.StatusMethodNotAllowed)
				return
			}
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if r.Form.Get("grant_type") != "authorization_code" {
				http.Error(w, "invalid grant_type", http.StatusBadRequest)
				return
			}
			if r.Form.Get("code") != expectedCode {
				http.Error(w, "invalid code", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  expectedAccessToken,
				"token_type":    "Bearer",
				"refresh_token": expectedRefreshToken,
				"expires_in":    3600,
			})

		case "/userinfo":
			atomic.AddInt32(&userInfoRequested, 1)
			if r.Method != http.MethodGet {
				http.Error(w, "expected GET", http.StatusMethodNotAllowed)
				return
			}
			authHeader := r.Header.Get("Authorization")
			expectedHeader := "Bearer " + expectedAccessToken
			if authHeader != expectedHeader {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":             "1234567890",
				"email":          expectedEmail,
				"verified_email": true,
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := ManagerConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/oauth/google/callback",
		TokenURL:     server.URL + "/token",
		UserInfoURL:  server.URL + "/userinfo",
	}

	mgr := NewManager(cfg)

	beforeUnix := time.Now().Unix()
	binding, err := mgr.ExchangeCode(context.Background(), expectedCode)
	afterUnix := time.Now().Unix()

	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}
	if binding == nil {
		t.Fatal("expected non-nil CalendarBinding")
	}

	if binding.Email != expectedEmail {
		t.Errorf("expected email %q, got %q", expectedEmail, binding.Email)
	}
	if binding.CalendarId != "primary" {
		t.Errorf("expected calendar_id 'primary', got %q", binding.CalendarId)
	}
	if binding.AccessToken != expectedAccessToken {
		t.Errorf("expected access_token %q, got %q", expectedAccessToken, binding.AccessToken)
	}
	if binding.RefreshToken != expectedRefreshToken {
		t.Errorf("expected refresh_token %q, got %q", expectedRefreshToken, binding.RefreshToken)
	}
	if binding.TokenExpiryUnix < beforeUnix+3500 || binding.TokenExpiryUnix > afterUnix+3700 {
		t.Errorf("unexpected token_expiry_unix: %d", binding.TokenExpiryUnix)
	}
	if binding.LinkedAtUnix < beforeUnix || binding.LinkedAtUnix > afterUnix {
		t.Errorf("unexpected linked_at_unix: %d (expected between %d and %d)", binding.LinkedAtUnix, beforeUnix, afterUnix)
	}

	if atomic.LoadInt32(&tokenRequested) != 1 {
		t.Errorf("expected 1 token request, got %d", tokenRequested)
	}
	if atomic.LoadInt32(&userInfoRequested) != 1 {
		t.Errorf("expected 1 userinfo request, got %d", userInfoRequested)
	}
}

func TestExchangeCode_EmptyCode(t *testing.T) {
	mgr := NewManager(ManagerConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost/callback",
	})

	_, err := mgr.ExchangeCode(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty authorization code, got nil")
	}
}

func TestExchangeCode_ExpiredOrInvalidCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":             "invalid_grant",
			"error_description": "Malformed auth code.",
		})
	}))
	defer server.Close()

	mgr := NewManager(ManagerConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     server.URL,
		UserInfoURL:  server.URL,
	})

	_, err := mgr.ExchangeCode(context.Background(), "expired-or-bad-code")
	if err == nil {
		t.Fatal("expected error for invalid_grant response, got nil")
	}
}

func TestExchangeCode_UserInfoFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "valid-token",
				"token_type":    "Bearer",
				"refresh_token": "valid-refresh",
				"expires_in":    3600,
			})
		case "/userinfo":
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	mgr := NewManager(ManagerConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     server.URL + "/token",
		UserInfoURL:  server.URL + "/userinfo",
	})

	_, err := mgr.ExchangeCode(context.Background(), "good-code")
	if err == nil {
		t.Fatal("expected error when userinfo endpoint fails with 500, got nil")
	}
}

func TestExchangeCode_UserInfoMissingEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "valid-token",
				"token_type":    "Bearer",
				"refresh_token": "valid-refresh",
				"expires_in":    3600,
			})
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "12345",
				// email is empty / omitted
			})
		}
	}))
	defer server.Close()

	mgr := NewManager(ManagerConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     server.URL + "/token",
		UserInfoURL:  server.URL + "/userinfo",
	})

	_, err := mgr.ExchangeCode(context.Background(), "good-code")
	if err == nil {
		t.Fatal("expected error when userinfo response has missing email, got nil")
	}
}

func TestExchangeCode_NetworkFailure(t *testing.T) {
	mgr := NewManager(ManagerConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     "http://127.0.0.1:54321/unreachable",
		UserInfoURL:  "http://127.0.0.1:54321/unreachable",
	})

	_, err := mgr.ExchangeCode(context.Background(), "good-code")
	if err == nil {
		t.Fatal("expected error for network failure, got nil")
	}
}

func TestExchangeCode_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr := NewManager(ManagerConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     "http://localhost/token",
		UserInfoURL:  "http://localhost/userinfo",
	})

	_, err := mgr.ExchangeCode(ctx, "any-code")
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

func TestExchangeCode_TimeoutEnforcement(t *testing.T) {
	// A server that hangs for longer than the timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "token",
		})
	}))
	defer server.Close()

	mgr := NewManager(ManagerConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     server.URL,
		UserInfoURL:  server.URL,
	})

	// Pass a context with a 50ms deadline to verify timeout cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := mgr.ExchangeCode(ctx, "any-code")
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected error on context timeout, got nil")
	}
	if duration > 150*time.Millisecond {
		t.Errorf("expected call to abort near 50ms, took %v", duration)
	}
}
