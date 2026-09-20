package config

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.BusyBarHost != "192.168.68.196" {
		t.Errorf("expected BusyBarHost %q, got %q", "192.168.68.196", cfg.BusyBarHost)
	}
	if cfg.BusyBarPort != 80 {
		t.Errorf("expected BusyBarPort 80, got %d", cfg.BusyBarPort)
	}
	if cfg.BusyBarDeviceID != "busybar" {
		t.Errorf("expected BusyBarDeviceID %q, got %q", "busybar", cfg.BusyBarDeviceID)
	}
	if cfg.HassURL != "http://homeassistant.local:8123" {
		t.Errorf("expected HassURL %q, got %q", "http://homeassistant.local:8123", cfg.HassURL)
	}
	if cfg.HassToken != "" {
		t.Errorf("expected empty HassToken in default config, got %q", cfg.HassToken)
	}
	if cfg.HassEventType != "busybar_event" {
		t.Errorf("expected HassEventType %q, got %q", "busybar_event", cfg.HassEventType)
	}
	if cfg.HassTimeout != 5*time.Second {
		t.Errorf("expected HassTimeout 5s, got %v", cfg.HassTimeout)
	}
	if cfg.ForwardStateEvents != false {
		t.Errorf("expected ForwardStateEvents false, got %v", cfg.ForwardStateEvents)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected Port 8080, got %d", cfg.Port)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected ShutdownTimeout 10s, got %v", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected LogLevel %q, got %q", "info", cfg.LogLevel)
	}
	if cfg.GoogleClientID != "" {
		t.Errorf("expected empty GoogleClientID in default config, got %q", cfg.GoogleClientID)
	}
	if cfg.GoogleClientSecret != "" {
		t.Errorf("expected empty GoogleClientSecret in default config, got %q", cfg.GoogleClientSecret)
	}
	if cfg.GoogleRedirectURL != "http://localhost:8080/oauth/google/callback" {
		t.Errorf("expected GoogleRedirectURL %q, got %q", "http://localhost:8080/oauth/google/callback", cfg.GoogleRedirectURL)
	}
	if cfg.CalendarStorePath != "/data/calendar_binding.pb" {
		t.Errorf("expected CalendarStorePath %q, got %q", "/data/calendar_binding.pb", cfg.CalendarStorePath)
	}
	if cfg.EnableOutboundPush != false {
		t.Errorf("expected EnableOutboundPush false, got %v", cfg.EnableOutboundPush)
	}
	if cfg.BusyBarAPIKey != "" {
		t.Errorf("expected empty BusyBarAPIKey in default config, got %q", cfg.BusyBarAPIKey)
	}
	if cfg.PushTimeout != 3*time.Second {
		t.Errorf("expected PushTimeout 3s, got %v", cfg.PushTimeout)
	}
}

func TestParse_DefaultsWithToken(t *testing.T) {
	cfg, err := Parse([]string{"-hass-token", "token123"})
	if err != nil {
		t.Fatalf("unexpected error parsing defaults with token flag: %v", err)
	}

	if cfg.BusyBarHost != "192.168.68.196" {
		t.Errorf("expected default BusyBarHost, got %q", cfg.BusyBarHost)
	}
	if cfg.BusyBarPort != 80 {
		t.Errorf("expected default BusyBarPort 80, got %d", cfg.BusyBarPort)
	}
	if cfg.BusyBarDeviceID != "busybar" {
		t.Errorf("expected default BusyBarDeviceID, got %q", cfg.BusyBarDeviceID)
	}
	if cfg.HassURL != "http://homeassistant.local:8123" {
		t.Errorf("expected default HassURL, got %q", cfg.HassURL)
	}
	if cfg.HassToken != "token123" {
		t.Errorf("expected HassToken %q, got %q", "token123", cfg.HassToken)
	}
	if cfg.HassEventType != "busybar_event" {
		t.Errorf("expected default HassEventType, got %q", cfg.HassEventType)
	}
	if cfg.HassTimeout != 5*time.Second {
		t.Errorf("expected default HassTimeout 5s, got %v", cfg.HassTimeout)
	}
	if cfg.ForwardStateEvents != false {
		t.Errorf("expected default ForwardStateEvents false, got %v", cfg.ForwardStateEvents)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected default Port 8080, got %d", cfg.Port)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected default ShutdownTimeout 10s, got %v", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected default LogLevel info, got %q", cfg.LogLevel)
	}
	if cfg.GoogleClientID != "" {
		t.Errorf("expected default GoogleClientID empty, got %q", cfg.GoogleClientID)
	}
	if cfg.GoogleClientSecret != "" {
		t.Errorf("expected default GoogleClientSecret empty, got %q", cfg.GoogleClientSecret)
	}
	if cfg.GoogleRedirectURL != "http://localhost:8080/oauth/google/callback" {
		t.Errorf("expected default GoogleRedirectURL, got %q", cfg.GoogleRedirectURL)
	}
	if cfg.CalendarStorePath != "/data/calendar_binding.pb" {
		t.Errorf("expected default CalendarStorePath, got %q", cfg.CalendarStorePath)
	}
	if cfg.EnableOutboundPush != false {
		t.Errorf("expected default EnableOutboundPush false, got %v", cfg.EnableOutboundPush)
	}
	if cfg.BusyBarAPIKey != "" {
		t.Errorf("expected default BusyBarAPIKey empty, got %q", cfg.BusyBarAPIKey)
	}
	if cfg.PushTimeout != 3*time.Second {
		t.Errorf("expected default PushTimeout 3s, got %v", cfg.PushTimeout)
	}
}

func TestParse_EnvironmentVariables(t *testing.T) {
	t.Setenv("BUSYBAR_HOST", "10.0.0.50")
	t.Setenv("BUSYBAR_PORT", "8081")
	t.Setenv("BUSYBAR_DEVICE_ID", "custom-device")
	t.Setenv("HASS_URL", "https://ha.internal:8123")
	t.Setenv("HASS_TOKEN", "env-secret-token")
	t.Setenv("HASS_EVENT_TYPE", "custom_event")
	t.Setenv("HASS_TIMEOUT", "12s")
	t.Setenv("FORWARD_STATE_EVENTS", "true")
	t.Setenv("PORT", "9000")
	t.Setenv("SHUTDOWN_TIMEOUT", "20s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("GOOGLE_CLIENT_ID", "env-client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "env-client-secret")
	t.Setenv("GOOGLE_REDIRECT_URL", "https://example.com/oauth/callback")
	t.Setenv("CALENDAR_STORE_PATH", "/tmp/calendar.pb")
	t.Setenv("ENABLE_OUTBOUND_PUSH", "true")
	t.Setenv("BUSYBAR_API_KEY", "env-secret-api-key")
	t.Setenv("PUSH_TIMEOUT", "8s")

	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("unexpected error parsing environment variables: %v", err)
	}

	if cfg.BusyBarHost != "10.0.0.50" {
		t.Errorf("expected BusyBarHost 10.0.0.50, got %q", cfg.BusyBarHost)
	}
	if cfg.BusyBarPort != 8081 {
		t.Errorf("expected BusyBarPort 8081, got %d", cfg.BusyBarPort)
	}
	if cfg.BusyBarDeviceID != "custom-device" {
		t.Errorf("expected BusyBarDeviceID custom-device, got %q", cfg.BusyBarDeviceID)
	}
	if cfg.HassURL != "https://ha.internal:8123" {
		t.Errorf("expected HassURL https://ha.internal:8123, got %q", cfg.HassURL)
	}
	if cfg.HassToken != "env-secret-token" {
		t.Errorf("expected HassToken env-secret-token, got %q", cfg.HassToken)
	}
	if cfg.HassEventType != "custom_event" {
		t.Errorf("expected HassEventType custom_event, got %q", cfg.HassEventType)
	}
	if cfg.HassTimeout != 12*time.Second {
		t.Errorf("expected HassTimeout 12s, got %v", cfg.HassTimeout)
	}
	if cfg.ForwardStateEvents != true {
		t.Errorf("expected ForwardStateEvents true, got %v", cfg.ForwardStateEvents)
	}
	if cfg.Port != 9000 {
		t.Errorf("expected Port 9000, got %d", cfg.Port)
	}
	if cfg.ShutdownTimeout != 20*time.Second {
		t.Errorf("expected ShutdownTimeout 20s, got %v", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected LogLevel debug, got %q", cfg.LogLevel)
	}
	if cfg.GoogleClientID != "env-client-id" {
		t.Errorf("expected GoogleClientID %q, got %q", "env-client-id", cfg.GoogleClientID)
	}
	if cfg.GoogleClientSecret != "env-client-secret" {
		t.Errorf("expected GoogleClientSecret %q, got %q", "env-client-secret", cfg.GoogleClientSecret)
	}
	if cfg.GoogleRedirectURL != "https://example.com/oauth/callback" {
		t.Errorf("expected GoogleRedirectURL %q, got %q", "https://example.com/oauth/callback", cfg.GoogleRedirectURL)
	}
	if cfg.CalendarStorePath != "/tmp/calendar.pb" {
		t.Errorf("expected CalendarStorePath %q, got %q", "/tmp/calendar.pb", cfg.CalendarStorePath)
	}
	if cfg.EnableOutboundPush != true {
		t.Errorf("expected EnableOutboundPush true, got %v", cfg.EnableOutboundPush)
	}
	if cfg.BusyBarAPIKey != "env-secret-api-key" {
		t.Errorf("expected BusyBarAPIKey env-secret-api-key, got %q", cfg.BusyBarAPIKey)
	}
	if cfg.PushTimeout != 8*time.Second {
		t.Errorf("expected PushTimeout 8s, got %v", cfg.PushTimeout)
	}
}

func TestParse_CLIOverridePrecedence(t *testing.T) {
	// Set environment variables
	t.Setenv("BUSYBAR_HOST", "10.0.0.50")
	t.Setenv("BUSYBAR_PORT", "8081")
	t.Setenv("BUSYBAR_DEVICE_ID", "device-from-env")
	t.Setenv("HASS_URL", "https://env.ha:8123")
	t.Setenv("HASS_TOKEN", "env-token")
	t.Setenv("HASS_EVENT_TYPE", "env_event")
	t.Setenv("HASS_TIMEOUT", "12s")
	t.Setenv("FORWARD_STATE_EVENTS", "false")
	t.Setenv("PORT", "9000")
	t.Setenv("SHUTDOWN_TIMEOUT", "20s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("GOOGLE_CLIENT_ID", "env-client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "env-client-secret")
	t.Setenv("GOOGLE_REDIRECT_URL", "https://env.example.com/callback")
	t.Setenv("CALENDAR_STORE_PATH", "/env/calendar.pb")
	t.Setenv("ENABLE_OUTBOUND_PUSH", "false")
	t.Setenv("BUSYBAR_API_KEY", "env-api-key")
	t.Setenv("PUSH_TIMEOUT", "10s")

	// Pass CLI args to override everything
	args := []string{
		"-busybar-host", "10.0.0.99",
		"-busybar-port", "9091",
		"-busybar-device-id", "device-from-cli",
		"-hass-url", "https://cli.ha:8123",
		"-hass-token", "cli-token",
		"-hass-event-type", "cli_event",
		"-hass-timeout", "3s",
		"-forward-state-events=true",
		"-port", "8888",
		"-shutdown-timeout", "15s",
		"-log-level", "warn",
		"-google-client-id", "cli-client-id",
		"-google-client-secret", "cli-client-secret",
		"-google-redirect-url", "https://cli.example.com/callback",
		"-calendar-store-path", "/cli/calendar.pb",
		"-enable-outbound-push=true",
		"-busybar-api-key", "cli-api-key",
		"-push-timeout", "4s",
	}

	cfg, err := Parse(args)
	if err != nil {
		t.Fatalf("unexpected error parsing CLI overrides: %v", err)
	}

	if cfg.BusyBarHost != "10.0.0.99" {
		t.Errorf("expected BusyBarHost 10.0.0.99 from CLI, got %q", cfg.BusyBarHost)
	}
	if cfg.BusyBarPort != 9091 {
		t.Errorf("expected BusyBarPort 9091 from CLI, got %d", cfg.BusyBarPort)
	}
	if cfg.BusyBarDeviceID != "device-from-cli" {
		t.Errorf("expected BusyBarDeviceID device-from-cli from CLI, got %q", cfg.BusyBarDeviceID)
	}
	if cfg.HassURL != "https://cli.ha:8123" {
		t.Errorf("expected HassURL https://cli.ha:8123 from CLI, got %q", cfg.HassURL)
	}
	if cfg.HassToken != "cli-token" {
		t.Errorf("expected HassToken cli-token from CLI, got %q", cfg.HassToken)
	}
	if cfg.HassEventType != "cli_event" {
		t.Errorf("expected HassEventType cli_event from CLI, got %q", cfg.HassEventType)
	}
	if cfg.HassTimeout != 3*time.Second {
		t.Errorf("expected HassTimeout 3s from CLI, got %v", cfg.HassTimeout)
	}
	if cfg.ForwardStateEvents != true {
		t.Errorf("expected ForwardStateEvents true from CLI, got %v", cfg.ForwardStateEvents)
	}
	if cfg.Port != 8888 {
		t.Errorf("expected Port 8888 from CLI, got %d", cfg.Port)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("expected ShutdownTimeout 15s from CLI, got %v", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("expected LogLevel warn from CLI, got %q", cfg.LogLevel)
	}
	if cfg.GoogleClientID != "cli-client-id" {
		t.Errorf("expected GoogleClientID cli-client-id from CLI, got %q", cfg.GoogleClientID)
	}
	if cfg.GoogleClientSecret != "cli-client-secret" {
		t.Errorf("expected GoogleClientSecret cli-client-secret from CLI, got %q", cfg.GoogleClientSecret)
	}
	if cfg.GoogleRedirectURL != "https://cli.example.com/callback" {
		t.Errorf("expected GoogleRedirectURL https://cli.example.com/callback from CLI, got %q", cfg.GoogleRedirectURL)
	}
	if cfg.CalendarStorePath != "/cli/calendar.pb" {
		t.Errorf("expected CalendarStorePath /cli/calendar.pb from CLI, got %q", cfg.CalendarStorePath)
	}
	if cfg.EnableOutboundPush != true {
		t.Errorf("expected EnableOutboundPush true from CLI, got %v", cfg.EnableOutboundPush)
	}
	if cfg.BusyBarAPIKey != "cli-api-key" {
		t.Errorf("expected BusyBarAPIKey cli-api-key from CLI, got %q", cfg.BusyBarAPIKey)
	}
	if cfg.PushTimeout != 4*time.Second {
		t.Errorf("expected PushTimeout 4s from CLI, got %v", cfg.PushTimeout)
	}
}

func TestParse_UnderscoreFlags(t *testing.T) {
	args := []string{
		"-busybar_host", "192.168.1.5",
		"-busybar_port", "88",
		"-busybar_device_id", "bb-under",
		"-hass_url", "https://under.ha:8123",
		"-hass_token", "under-token",
		"-hass_event_type", "under_event",
		"-hass_timeout", "7s",
		"-forward_state_events=true",
		"-shutdown_timeout", "14s",
		"-log_level", "error",
		"-google_client_id", "under-client-id",
		"-google_client_secret", "under-client-secret",
		"-google_redirect_url", "https://under.example.com/callback",
		"-calendar_store_path", "/under/calendar.pb",
		"-enable_outbound_push=true",
		"-busybar_api_key", "under-push-key",
		"-push_timeout", "8s",
	}

	cfg, err := Parse(args)
	if err != nil {
		t.Fatalf("unexpected error parsing underscore flags: %v", err)
	}

	if cfg.BusyBarHost != "192.168.1.5" {
		t.Errorf("expected BusyBarHost 192.168.1.5, got %q", cfg.BusyBarHost)
	}
	if cfg.BusyBarPort != 88 {
		t.Errorf("expected BusyBarPort 88, got %d", cfg.BusyBarPort)
	}
	if cfg.BusyBarDeviceID != "bb-under" {
		t.Errorf("expected BusyBarDeviceID bb-under, got %q", cfg.BusyBarDeviceID)
	}
	if cfg.HassURL != "https://under.ha:8123" {
		t.Errorf("expected HassURL https://under.ha:8123, got %q", cfg.HassURL)
	}
	if cfg.HassToken != "under-token" {
		t.Errorf("expected HassToken under-token, got %q", cfg.HassToken)
	}
	if cfg.HassEventType != "under_event" {
		t.Errorf("expected HassEventType under_event, got %q", cfg.HassEventType)
	}
	if cfg.HassTimeout != 7*time.Second {
		t.Errorf("expected HassTimeout 7s, got %v", cfg.HassTimeout)
	}
	if cfg.ForwardStateEvents != true {
		t.Errorf("expected ForwardStateEvents true, got %v", cfg.ForwardStateEvents)
	}
	if cfg.ShutdownTimeout != 14*time.Second {
		t.Errorf("expected ShutdownTimeout 14s, got %v", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != "error" {
		t.Errorf("expected LogLevel error, got %q", cfg.LogLevel)
	}
	if cfg.GoogleClientID != "under-client-id" {
		t.Errorf("expected GoogleClientID under-client-id, got %q", cfg.GoogleClientID)
	}
	if cfg.GoogleClientSecret != "under-client-secret" {
		t.Errorf("expected GoogleClientSecret under-client-secret, got %q", cfg.GoogleClientSecret)
	}
	if cfg.GoogleRedirectURL != "https://under.example.com/callback" {
		t.Errorf("expected GoogleRedirectURL https://under.example.com/callback, got %q", cfg.GoogleRedirectURL)
	}
	if cfg.CalendarStorePath != "/under/calendar.pb" {
		t.Errorf("expected CalendarStorePath /under/calendar.pb, got %q", cfg.CalendarStorePath)
	}
	if cfg.EnableOutboundPush != true {
		t.Errorf("expected EnableOutboundPush true from CLI alias, got %v", cfg.EnableOutboundPush)
	}
	if cfg.BusyBarAPIKey != "under-push-key" {
		t.Errorf("expected BusyBarAPIKey under-push-key from CLI alias, got %q", cfg.BusyBarAPIKey)
	}
	if cfg.PushTimeout != 8*time.Second {
		t.Errorf("expected PushTimeout 8s from CLI alias, got %v", cfg.PushTimeout)
	}
}

func TestParse_InvalidEnvVars(t *testing.T) {
	tests := []struct {
		name    string
		envKey  string
		envVal  string
		errSub  string
	}{
		{
			name:   "invalid PORT",
			envKey: "PORT",
			envVal: "not-an-int",
			errSub: "PORT",
		},
		{
			name:   "invalid BUSYBAR_PORT",
			envKey: "BUSYBAR_PORT",
			envVal: "invalid",
			errSub: "BUSYBAR_PORT",
		},
		{
			name:   "invalid HASS_TIMEOUT",
			envKey: "HASS_TIMEOUT",
			envVal: "invalid-duration",
			errSub: "HASS_TIMEOUT",
		},
		{
			name:   "invalid SHUTDOWN_TIMEOUT",
			envKey: "SHUTDOWN_TIMEOUT",
			envVal: "invalid-duration",
			errSub: "SHUTDOWN_TIMEOUT",
		},
		{
			name:   "invalid FORWARD_STATE_EVENTS",
			envKey: "FORWARD_STATE_EVENTS",
			envVal: "not-a-bool",
			errSub: "FORWARD_STATE_EVENTS",
		},
		{
			name:   "invalid ENABLE_OUTBOUND_PUSH",
			envKey: "ENABLE_OUTBOUND_PUSH",
			envVal: "not-a-bool",
			errSub: "ENABLE_OUTBOUND_PUSH",
		},
		{
			name:   "invalid PUSH_TIMEOUT",
			envKey: "PUSH_TIMEOUT",
			envVal: "invalid-duration",
			errSub: "PUSH_TIMEOUT",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HASS_TOKEN", "valid-token")
			t.Setenv(tc.envKey, tc.envVal)

			_, err := Parse(nil)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(strings.ToUpper(err.Error()), tc.errSub) {
				t.Errorf("expected error message to contain %q, got: %v", tc.errSub, err)
			}
		})
	}
}

func TestValidate_Valid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HassToken = "super-secret"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config to pass validation, got: %v", err)
	}
}

func TestValidate_MissingHassToken(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HassToken = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty HassToken, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "token") {
		t.Errorf("expected error to mention 'token', got: %v", err)
	}
}

func TestValidate_HassURL(t *testing.T) {
	tests := []struct {
		name    string
		hassURL string
	}{
		{name: "empty URL", hassURL: ""},
		{name: "whitespace only", hassURL: "   "},
		{name: "ftp scheme", hassURL: "ftp://homeassistant.local:8123"},
		{name: "ws scheme", hassURL: "ws://homeassistant.local:8123"},
		{name: "missing scheme", hassURL: "homeassistant.local:8123"},
		{name: "missing host", hassURL: "http://"},
		{name: "malformed URL", hassURL: "http://[invalid-ipv6"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.HassToken = "token"
			cfg.HassURL = tc.hassURL

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for invalid HassURL %q, got nil", tc.hassURL)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "hass_url") && !strings.Contains(strings.ToLower(err.Error()), "url") {
				t.Errorf("expected error to mention url, got: %v", err)
			}
		})
	}
}

func TestValidate_Ports(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *AppConfig)
		errSub string
	}{
		{
			name: "zero Port",
			mutate: func(c *AppConfig) {
				c.Port = 0
			},
			errSub: "port",
		},
		{
			name: "negative Port",
			mutate: func(c *AppConfig) {
				c.Port = -1
			},
			errSub: "port",
		},
		{
			name: "Port above 65535",
			mutate: func(c *AppConfig) {
				c.Port = 65536
			},
			errSub: "port",
		},
		{
			name: "zero BusyBarPort",
			mutate: func(c *AppConfig) {
				c.BusyBarPort = 0
			},
			errSub: "busybar_port",
		},
		{
			name: "negative BusyBarPort",
			mutate: func(c *AppConfig) {
				c.BusyBarPort = -10
			},
			errSub: "busybar_port",
		},
		{
			name: "BusyBarPort above 65535",
			mutate: func(c *AppConfig) {
				c.BusyBarPort = 70000
			},
			errSub: "busybar_port",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.HassToken = "token"
			tc.mutate(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.errSub) {
				t.Errorf("expected error to contain %q, got: %v", tc.errSub, err)
			}
		})
	}
}

func TestValidate_Timeouts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *AppConfig)
		errSub string
	}{
		{
			name: "zero HassTimeout",
			mutate: func(c *AppConfig) {
				c.HassTimeout = 0
			},
			errSub: "hass_timeout",
		},
		{
			name: "negative HassTimeout",
			mutate: func(c *AppConfig) {
				c.HassTimeout = -1 * time.Second
			},
			errSub: "hass_timeout",
		},
		{
			name: "zero ShutdownTimeout",
			mutate: func(c *AppConfig) {
				c.ShutdownTimeout = 0
			},
			errSub: "shutdown_timeout",
		},
		{
			name: "negative ShutdownTimeout",
			mutate: func(c *AppConfig) {
				c.ShutdownTimeout = -5 * time.Second
			},
			errSub: "shutdown_timeout",
		},
		{
			name: "zero PushTimeout",
			mutate: func(c *AppConfig) {
				c.PushTimeout = 0
			},
			errSub: "push_timeout",
		},
		{
			name: "negative PushTimeout",
			mutate: func(c *AppConfig) {
				c.PushTimeout = -1 * time.Second
			},
			errSub: "push_timeout",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.HassToken = "token"
			tc.mutate(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.errSub) {
				t.Errorf("expected error to contain %q, got: %v", tc.errSub, err)
			}
		})
	}
}

func TestParse_FailFastWithoutToken(t *testing.T) {
	_, err := Parse(nil)
	if err == nil {
		t.Fatal("expected Parse to fail fast when HASS_TOKEN is missing, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "token") {
		t.Errorf("expected error to mention token, got: %v", err)
	}
}

func TestAppConfig_IsOAuthConfigured(t *testing.T) {
	tests := []struct {
		name     string
		clientID string
		secret   string
		expected bool
	}{
		{
			name:     "both empty",
			clientID: "",
			secret:   "",
			expected: false,
		},
		{
			name:     "client ID only",
			clientID: "client-id",
			secret:   "",
			expected: false,
		},
		{
			name:     "secret only",
			clientID: "",
			secret:   "secret-val",
			expected: false,
		},
		{
			name:     "both whitespace only",
			clientID: "   ",
			secret:   "   ",
			expected: false,
		},
		{
			name:     "both configured",
			clientID: "my-client-id",
			secret:   "my-secret",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.GoogleClientID = tc.clientID
			cfg.GoogleClientSecret = tc.secret
			if got := cfg.IsOAuthConfigured(); got != tc.expected {
				t.Errorf("IsOAuthConfigured() = %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestValidate_GoogleRedirectURL(t *testing.T) {
	tests := []struct {
		name        string
		redirectURL string
		expectErr   bool
		errSub      string
	}{
		{
			name:        "empty URL (allowed when unspecified)",
			redirectURL: "",
			expectErr:   false,
		},
		{
			name:        "whitespace only URL",
			redirectURL: "   ",
			expectErr:   false,
		},
		{
			name:        "valid http URL",
			redirectURL: "http://localhost:8080/oauth/google/callback",
			expectErr:   false,
		},
		{
			name:        "valid https URL",
			redirectURL: "https://example.com/oauth/callback",
			expectErr:   false,
		},
		{
			name:        "invalid scheme ftp",
			redirectURL: "ftp://localhost:8080/oauth/callback",
			expectErr:   true,
			errSub:      "google_redirect_url",
		},
		{
			name:        "invalid scheme ws",
			redirectURL: "ws://localhost:8080/oauth/callback",
			expectErr:   true,
			errSub:      "google_redirect_url",
		},
		{
			name:        "missing host",
			redirectURL: "http://",
			expectErr:   true,
			errSub:      "google_redirect_url",
		},
		{
			name:        "malformed URL",
			redirectURL: "http://[invalid-ipv6",
			expectErr:   true,
			errSub:      "google_redirect_url",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.HassToken = "valid-token"
			cfg.GoogleRedirectURL = tc.redirectURL

			err := cfg.Validate()
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tc.redirectURL)
				}
				if !strings.Contains(strings.ToLower(err.Error()), tc.errSub) {
					t.Errorf("expected error to contain %q, got: %v", tc.errSub, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for %q: %v", tc.redirectURL, err)
				}
			}
		})
	}
}

func TestValidate_OAuthNotRequired(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HassToken = "valid-token"
	cfg.GoogleClientID = ""
	cfg.GoogleClientSecret = ""

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected Validate to succeed without OAuth credentials, got: %v", err)
	}

	if cfg.IsOAuthConfigured() {
		t.Fatal("expected IsOAuthConfigured to be false")
	}
}

func TestOutboundPushConfig(t *testing.T) {
	t.Run("default push settings", func(t *testing.T) {
		cfg := DefaultConfig()
		if cfg.EnableOutboundPush != false {
			t.Errorf("expected EnableOutboundPush false, got %v", cfg.EnableOutboundPush)
		}
		if cfg.BusyBarAPIKey != "" {
			t.Errorf("expected BusyBarAPIKey empty, got %q", cfg.BusyBarAPIKey)
		}
		if cfg.PushTimeout != 3*time.Second {
			t.Errorf("expected PushTimeout 3s, got %v", cfg.PushTimeout)
		}
	})

	t.Run("environment variable overrides for push settings", func(t *testing.T) {
		t.Setenv("HASS_TOKEN", "token")
		t.Setenv("ENABLE_OUTBOUND_PUSH", "true")
		t.Setenv("BUSYBAR_API_KEY", "key-from-env")
		t.Setenv("PUSH_TIMEOUT", "5s")

		cfg, err := Parse(nil)
		if err != nil {
			t.Fatalf("unexpected error parsing env vars: %v", err)
		}
		if !cfg.EnableOutboundPush {
			t.Errorf("expected EnableOutboundPush true, got %v", cfg.EnableOutboundPush)
		}
		if cfg.BusyBarAPIKey != "key-from-env" {
			t.Errorf("expected BusyBarAPIKey 'key-from-env', got %q", cfg.BusyBarAPIKey)
		}
		if cfg.PushTimeout != 5*time.Second {
			t.Errorf("expected PushTimeout 5s, got %v", cfg.PushTimeout)
		}
	})

	t.Run("CLI flag overrides taking precedence over environment variables", func(t *testing.T) {
		t.Setenv("HASS_TOKEN", "token")
		t.Setenv("ENABLE_OUTBOUND_PUSH", "false")
		t.Setenv("BUSYBAR_API_KEY", "env-key")
		t.Setenv("PUSH_TIMEOUT", "10s")

		args := []string{
			"--enable-outbound-push=true",
			"--busybar-api-key", "cli-key",
			"--push-timeout", "2s",
		}
		cfg, err := Parse(args)
		if err != nil {
			t.Fatalf("unexpected error parsing CLI flags: %v", err)
		}
		if !cfg.EnableOutboundPush {
			t.Errorf("expected EnableOutboundPush true from CLI, got %v", cfg.EnableOutboundPush)
		}
		if cfg.BusyBarAPIKey != "cli-key" {
			t.Errorf("expected BusyBarAPIKey 'cli-key', got %q", cfg.BusyBarAPIKey)
		}
		if cfg.PushTimeout != 2*time.Second {
			t.Errorf("expected PushTimeout 2s, got %v", cfg.PushTimeout)
		}
	})

	t.Run("CLI underscore flag aliases", func(t *testing.T) {
		t.Setenv("HASS_TOKEN", "token")
		args := []string{
			"--enable_outbound_push=true",
			"--busybar_api_key", "alias-key",
			"--push_timeout", "9s",
		}
		cfg, err := Parse(args)
		if err != nil {
			t.Fatalf("unexpected error parsing CLI alias flags: %v", err)
		}
		if !cfg.EnableOutboundPush {
			t.Errorf("expected EnableOutboundPush true from CLI alias, got %v", cfg.EnableOutboundPush)
		}
		if cfg.BusyBarAPIKey != "alias-key" {
			t.Errorf("expected BusyBarAPIKey 'alias-key', got %q", cfg.BusyBarAPIKey)
		}
		if cfg.PushTimeout != 9*time.Second {
			t.Errorf("expected PushTimeout 9s, got %v", cfg.PushTimeout)
		}
	})

	t.Run("validation rejection when PushTimeout <= 0", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.HassToken = "token"

		cfg.PushTimeout = 0
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected error for PushTimeout=0, got nil")
		} else if !strings.Contains(strings.ToLower(err.Error()), "push_timeout") {
			t.Errorf("expected error mentioning push_timeout, got: %v", err)
		}

		cfg.PushTimeout = -1 * time.Second
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected error for negative PushTimeout, got nil")
		} else if !strings.Contains(strings.ToLower(err.Error()), "push_timeout") {
			t.Errorf("expected error mentioning push_timeout, got: %v", err)
		}
	})
}
