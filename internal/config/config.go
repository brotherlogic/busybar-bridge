package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrMissingHassToken indicates the mandatory Home Assistant token is missing or empty.
	ErrMissingHassToken = errors.New("hass_token is required")
	// ErrInvalidHassURL indicates the Home Assistant URL is invalid.
	ErrInvalidHassURL = errors.New("invalid hass_url: must be a valid http or https URL")
	// ErrInvalidPort indicates the server port is outside the 1-65535 range.
	ErrInvalidPort = errors.New("port must be between 1 and 65535")
	// ErrInvalidBusyBarPort indicates the Busy Bar port is outside the 1-65535 range.
	ErrInvalidBusyBarPort = errors.New("busybar_port must be between 1 and 65535")
	// ErrInvalidHassTimeout indicates the Home Assistant request timeout is not positive.
	ErrInvalidHassTimeout = errors.New("hass_timeout must be positive")
	// ErrInvalidShutdownTimeout indicates the shutdown timeout is not positive.
	ErrInvalidShutdownTimeout = errors.New("shutdown_timeout must be positive")
	// ErrInvalidGoogleRedirectURL indicates the Google OAuth redirect URL is invalid.
	ErrInvalidGoogleRedirectURL = errors.New("invalid google_redirect_url: must be a valid http or https URL")
	// ErrInvalidPushTimeout indicates the outbound push request timeout is not positive.
	ErrInvalidPushTimeout = errors.New("push_timeout must be positive")
)

// AppConfig defines application configuration parameters.
type AppConfig struct {
	BusyBarHost        string
	BusyBarPort        int
	BusyBarDeviceID    string
	HassURL            string
	HassToken          string
	HassEventType      string
	HassTimeout        time.Duration
	ForwardStateEvents bool
	Port               int
	ShutdownTimeout    time.Duration
	LogLevel           string
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	CalendarStorePath  string
	EnableOutboundPush bool
	BusyBarAPIKey      string
	PushTimeout        time.Duration
}

// DefaultConfig returns a new AppConfig populated with default values.
func DefaultConfig() *AppConfig {
	return &AppConfig{
		BusyBarHost:        "192.168.68.196",
		BusyBarPort:        80,
		BusyBarDeviceID:    "busybar",
		HassURL:            "http://homeassistant.local:8123",
		HassToken:          "",
		HassEventType:      "busybar_event",
		HassTimeout:        5 * time.Second,
		ForwardStateEvents: false,
		Port:               8080,
		ShutdownTimeout:    10 * time.Second,
		LogLevel:           "info",
		GoogleClientID:     "",
		GoogleClientSecret: "",
		GoogleRedirectURL:  "http://localhost:8080/oauth/google/callback",
		CalendarStorePath:  "/data/calendar_binding.pb",
		EnableOutboundPush: false,
		BusyBarAPIKey:      "",
		PushTimeout:        3 * time.Second,
	}
}

// IsOAuthConfigured returns true when both GoogleClientID and GoogleClientSecret are non-empty.
func (c *AppConfig) IsOAuthConfigured() bool {
	return strings.TrimSpace(c.GoogleClientID) != "" && strings.TrimSpace(c.GoogleClientSecret) != ""
}

// Validate checks that AppConfig satisfies all fail-fast invariants.
func (c *AppConfig) Validate() error {
	if strings.TrimSpace(c.HassToken) == "" {
		return ErrMissingHassToken
	}

	if strings.TrimSpace(c.HassURL) == "" {
		return fmt.Errorf("%w: empty URL", ErrInvalidHassURL)
	}

	u, err := url.ParseRequestURI(c.HassURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidHassURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q not supported", ErrInvalidHassURL, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: host is empty", ErrInvalidHassURL)
	}

	if strings.TrimSpace(c.GoogleRedirectURL) != "" {
		gu, err := url.ParseRequestURI(c.GoogleRedirectURL)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidGoogleRedirectURL, err)
		}
		if gu.Scheme != "http" && gu.Scheme != "https" {
			return fmt.Errorf("%w: scheme %q not supported", ErrInvalidGoogleRedirectURL, gu.Scheme)
		}
		if gu.Host == "" {
			return fmt.Errorf("%w: host is empty", ErrInvalidGoogleRedirectURL)
		}
	}

	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("%w: got %d", ErrInvalidPort, c.Port)
	}
	if c.BusyBarPort < 1 || c.BusyBarPort > 65535 {
		return fmt.Errorf("%w: got %d", ErrInvalidBusyBarPort, c.BusyBarPort)
	}

	if c.HassTimeout <= 0 {
		return ErrInvalidHassTimeout
	}
	if c.ShutdownTimeout <= 0 {
		return ErrInvalidShutdownTimeout
	}
	if c.PushTimeout <= 0 {
		return ErrInvalidPushTimeout
	}

	return nil
}

// Parse parses configuration from default values, environment variables, and CLI arguments.
// Precedence: CLI flags > environment variables > defaults.
// Enforces fail-fast validation prior to returning.
func Parse(args []string) (*AppConfig, error) {
	cfg := DefaultConfig()

	// Load from environment variables
	if v := os.Getenv("BUSYBAR_HOST"); v != "" {
		cfg.BusyBarHost = v
	}
	if v := os.Getenv("BUSYBAR_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid BUSYBAR_PORT: %w", err)
		}
		cfg.BusyBarPort = p
	}
	if v := os.Getenv("BUSYBAR_DEVICE_ID"); v != "" {
		cfg.BusyBarDeviceID = v
	}
	if v := os.Getenv("HASS_URL"); v != "" {
		cfg.HassURL = v
	}
	if v := os.Getenv("HASS_TOKEN"); v != "" {
		cfg.HassToken = v
	}
	if v := os.Getenv("HASS_EVENT_TYPE"); v != "" {
		cfg.HassEventType = v
	}
	if v := os.Getenv("HASS_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid HASS_TIMEOUT: %w", err)
		}
		cfg.HassTimeout = d
	}
	if v := os.Getenv("FORWARD_STATE_EVENTS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid FORWARD_STATE_EVENTS: %w", err)
		}
		cfg.ForwardStateEvents = b
	}
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT: %w", err)
		}
		cfg.Port = p
	}
	if v := os.Getenv("SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid SHUTDOWN_TIMEOUT: %w", err)
		}
		cfg.ShutdownTimeout = d
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("GOOGLE_CLIENT_ID"); v != "" {
		cfg.GoogleClientID = v
	}
	if v := os.Getenv("GOOGLE_CLIENT_SECRET"); v != "" {
		cfg.GoogleClientSecret = v
	}
	if v := os.Getenv("GOOGLE_REDIRECT_URL"); v != "" {
		cfg.GoogleRedirectURL = v
	}
	if v := os.Getenv("CALENDAR_STORE_PATH"); v != "" {
		cfg.CalendarStorePath = v
	}
	if v := os.Getenv("ENABLE_OUTBOUND_PUSH"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid ENABLE_OUTBOUND_PUSH: %w", err)
		}
		cfg.EnableOutboundPush = b
	}
	if v := os.Getenv("BUSYBAR_API_KEY"); v != "" {
		cfg.BusyBarAPIKey = v
	}
	if v := os.Getenv("PUSH_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid PUSH_TIMEOUT: %w", err)
		}
		cfg.PushTimeout = d
	}

	// Parse CLI flags
	fs := flag.NewFlagSet("busybar-bridge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&cfg.BusyBarHost, "busybar-host", cfg.BusyBarHost, "BusyBar host address")
	fs.StringVar(&cfg.BusyBarHost, "busybar_host", cfg.BusyBarHost, "BusyBar host address (alias)")
	fs.IntVar(&cfg.BusyBarPort, "busybar-port", cfg.BusyBarPort, "BusyBar port")
	fs.IntVar(&cfg.BusyBarPort, "busybar_port", cfg.BusyBarPort, "BusyBar port (alias)")
	fs.StringVar(&cfg.BusyBarDeviceID, "busybar-device-id", cfg.BusyBarDeviceID, "BusyBar device ID")
	fs.StringVar(&cfg.BusyBarDeviceID, "busybar_device_id", cfg.BusyBarDeviceID, "BusyBar device ID (alias)")
	fs.StringVar(&cfg.HassURL, "hass-url", cfg.HassURL, "Home Assistant base URL")
	fs.StringVar(&cfg.HassURL, "hass_url", cfg.HassURL, "Home Assistant base URL (alias)")
	fs.StringVar(&cfg.HassToken, "hass-token", cfg.HassToken, "Home Assistant long-lived access token")
	fs.StringVar(&cfg.HassToken, "hass_token", cfg.HassToken, "Home Assistant long-lived access token (alias)")
	fs.StringVar(&cfg.HassEventType, "hass-event-type", cfg.HassEventType, "Home Assistant event type")
	fs.StringVar(&cfg.HassEventType, "hass_event_type", cfg.HassEventType, "Home Assistant event type (alias)")
	fs.DurationVar(&cfg.HassTimeout, "hass-timeout", cfg.HassTimeout, "Home Assistant request timeout")
	fs.DurationVar(&cfg.HassTimeout, "hass_timeout", cfg.HassTimeout, "Home Assistant request timeout (alias)")
	fs.BoolVar(&cfg.ForwardStateEvents, "forward-state-events", cfg.ForwardStateEvents, "Forward state events to Home Assistant")
	fs.BoolVar(&cfg.ForwardStateEvents, "forward_state_events", cfg.ForwardStateEvents, "Forward state events to Home Assistant (alias)")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "HTTP server listening port")
	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", cfg.ShutdownTimeout, "Graceful shutdown timeout")
	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown_timeout", cfg.ShutdownTimeout, "Graceful shutdown timeout (alias)")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "Logging level (debug, info, warn, error)")
	fs.StringVar(&cfg.LogLevel, "log_level", cfg.LogLevel, "Logging level (debug, info, warn, error) (alias)")
	fs.StringVar(&cfg.GoogleClientID, "google-client-id", cfg.GoogleClientID, "Google OAuth client ID")
	fs.StringVar(&cfg.GoogleClientID, "google_client_id", cfg.GoogleClientID, "Google OAuth client ID (alias)")
	fs.StringVar(&cfg.GoogleClientSecret, "google-client-secret", cfg.GoogleClientSecret, "Google OAuth client secret")
	fs.StringVar(&cfg.GoogleClientSecret, "google_client_secret", cfg.GoogleClientSecret, "Google OAuth client secret (alias)")
	fs.StringVar(&cfg.GoogleRedirectURL, "google-redirect-url", cfg.GoogleRedirectURL, "Google OAuth redirect URL")
	fs.StringVar(&cfg.GoogleRedirectURL, "google_redirect_url", cfg.GoogleRedirectURL, "Google OAuth redirect URL (alias)")
	fs.StringVar(&cfg.CalendarStorePath, "calendar-store-path", cfg.CalendarStorePath, "Calendar binding persistence store path")
	fs.StringVar(&cfg.CalendarStorePath, "calendar_store_path", cfg.CalendarStorePath, "Calendar binding persistence store path (alias)")
	fs.BoolVar(&cfg.EnableOutboundPush, "enable-outbound-push", cfg.EnableOutboundPush, "Enable outbound push to BusyBar")
	fs.BoolVar(&cfg.EnableOutboundPush, "enable_outbound_push", cfg.EnableOutboundPush, "Enable outbound push to BusyBar (alias)")
	fs.StringVar(&cfg.BusyBarAPIKey, "busybar-api-key", cfg.BusyBarAPIKey, "BusyBar outbound push API key")
	fs.StringVar(&cfg.BusyBarAPIKey, "busybar_api_key", cfg.BusyBarAPIKey, "BusyBar outbound push API key (alias)")
	fs.DurationVar(&cfg.PushTimeout, "push-timeout", cfg.PushTimeout, "BusyBar outbound push HTTP timeout")
	fs.DurationVar(&cfg.PushTimeout, "push_timeout", cfg.PushTimeout, "BusyBar outbound push HTTP timeout (alias)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
