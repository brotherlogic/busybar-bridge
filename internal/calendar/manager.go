package calendar

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/brotherlogic/busybar-bridge/pkg/pb"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	// ScopeCalendarEventsReadOnly grants read-only access to calendar events.
	ScopeCalendarEventsReadOnly = "https://www.googleapis.com/auth/calendar.events.readonly"

	// ScopeUserInfoEmail grants access to the user's primary email address.
	ScopeUserInfoEmail = "https://www.googleapis.com/auth/userinfo.email"

	// DefaultUserInfoURL is Google's OAuth 2.0 UserInfo API endpoint.
	DefaultUserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"

	// DefaultExchangeTimeout is the maximum duration permitted for token exchange and user profile retrieval.
	DefaultExchangeTimeout = 10 * time.Second
)

var (
	// ErrEmptyCode indicates that an empty authorization code was provided for exchange.
	ErrEmptyCode = errors.New("authorization code cannot be empty")

	// ErrMissingEmail indicates that the user profile retrieved from Google did not contain an email address.
	ErrMissingEmail = errors.New("user profile response did not contain an email address")
)

// ManagerConfig configures the OAuth Manager.
type ManagerConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
}

// Manager coordinates Google OAuth2 authorization and token exchange.
type Manager struct {
	cfg         ManagerConfig
	oauthConfig *oauth2.Config
	userInfoURL string
}

// NewManager constructs a new OAuth Manager configured for Google OAuth 2.0.
func NewManager(cfg ManagerConfig) *Manager {
	authURL := cfg.AuthURL
	if authURL == "" {
		authURL = google.Endpoint.AuthURL
	}
	tokenURL := cfg.TokenURL
	if tokenURL == "" {
		tokenURL = google.Endpoint.TokenURL
	}
	userInfoURL := cfg.UserInfoURL
	if userInfoURL == "" {
		userInfoURL = DefaultUserInfoURL
	}

	oauthConfig := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes: []string{
			ScopeCalendarEventsReadOnly,
			ScopeUserInfoEmail,
		},
		Endpoint: oauth2.Endpoint{
			AuthURL:  authURL,
			TokenURL: tokenURL,
		},
	}

	return &Manager{
		cfg:         cfg,
		oauthConfig: oauthConfig,
		userInfoURL: userInfoURL,
	}
}

// IsConfigured returns true if ClientID and ClientSecret are non-empty.
func (m *Manager) IsConfigured() bool {
	if m == nil {
		return false
	}
	return strings.TrimSpace(m.cfg.ClientID) != "" && strings.TrimSpace(m.cfg.ClientSecret) != ""
}

// GenerateAuthURL generates a cryptographically secure 32-byte CSRF state nonce
// and returns the Google OAuth authorization URL requesting offline access and consent prompt.
func (m *Manager) GenerateAuthURL() (string, string, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", "", fmt.Errorf("failed to generate random state nonce: %w", err)
	}

	state := base64.URLEncoding.EncodeToString(nonce)

	authURL := m.oauthConfig.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)

	return authURL, state, nil
}

// ExchangeCode exchanges an authorization code for an OAuth2 token and queries Google's
// userinfo endpoint to obtain the authenticated user's email. Enforces a 10-second timeout context.
func (m *Manager) ExchangeCode(ctx context.Context, code string) (*pb.CalendarBinding, error) {
	if strings.TrimSpace(code) == "" {
		return nil, ErrEmptyCode
	}

	if ctx == nil {
		ctx = context.Background()
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, DefaultExchangeTimeout)
	defer cancel()

	token, err := m.oauthConfig.Exchange(timeoutCtx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	client := m.oauthConfig.Client(timeoutCtx, token)
	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, m.userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create userinfo request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute userinfo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo request returned status %d: %s", resp.StatusCode, string(body))
	}

	var profile struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("failed to decode userinfo response: %w", err)
	}

	if strings.TrimSpace(profile.Email) == "" {
		return nil, ErrMissingEmail
	}

	var expiryUnix int64
	if !token.Expiry.IsZero() {
		expiryUnix = token.Expiry.Unix()
	}

	binding := &pb.CalendarBinding{
		Email:           profile.Email,
		CalendarId:      "primary",
		AccessToken:     token.AccessToken,
		RefreshToken:    token.RefreshToken,
		TokenExpiryUnix: expiryUnix,
		LinkedAtUnix:    time.Now().Unix(),
	}

	return binding, nil
}
