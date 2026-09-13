package hass

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.BaseURL != "http://homeassistant.local:8123" {
		t.Errorf("expected BaseURL %q, got %q", "http://homeassistant.local:8123", cfg.BaseURL)
	}
	if cfg.Token != "" {
		t.Errorf("expected empty Token in default config, got %q", cfg.Token)
	}
	if cfg.EventType != "busybar_event" {
		t.Errorf("expected EventType %q, got %q", "busybar_event", cfg.EventType)
	}
	if cfg.RequestTimeout != 2*time.Second {
		t.Errorf("expected RequestTimeout 2s, got %v", cfg.RequestTimeout)
	}
	if cfg.TTL != 2500*time.Millisecond {
		t.Errorf("expected TTL 2.5s (2500ms), got %v", cfg.TTL)
	}
	if cfg.MaxRetries != 2 {
		t.Errorf("expected MaxRetries 2, got %d", cfg.MaxRetries)
	}
	if cfg.BufferSize != 100 {
		t.Errorf("expected BufferSize 100, got %d", cfg.BufferSize)
	}
	if cfg.RetryBackoff != 100*time.Millisecond {
		t.Errorf("expected RetryBackoff 100ms, got %v", cfg.RetryBackoff)
	}
}

func TestValidate_Valid(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "default config with token",
			cfg: func() Config {
				c := DefaultConfig()
				c.Token = "valid-bearer-token"
				return c
			}(),
		},
		{
			name: "custom valid config with https",
			cfg: Config{
				BaseURL:        "https://ha.internal:8123",
				Token:          "bearer-xyz-123",
				EventType:      "custom_event",
				RequestTimeout: 5 * time.Second,
				TTL:            10 * time.Second,
				MaxRetries:     5,
				BufferSize:     500,
				RetryBackoff:   250 * time.Millisecond,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err != nil {
				t.Fatalf("expected valid config to pass validation, got error: %v", err)
			}
		})
	}
}

func TestValidate_MissingToken(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Token = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "token") {
		t.Errorf("expected error message to mention 'token', got: %v", err)
	}
}

func TestValidate_InvalidBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "empty URL", baseURL: ""},
		{name: "ftp scheme", baseURL: "ftp://homeassistant.local:8123"},
		{name: "ws scheme", baseURL: "ws://homeassistant.local:8123"},
		{name: "relative path", baseURL: "/api/events"},
		{name: "missing scheme", baseURL: "homeassistant.local:8123"},
		{name: "missing host", baseURL: "http://"},
		{name: "malformed URL", baseURL: "http://[invalid-ipv6"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Token = "some-token"
			cfg.BaseURL = tc.baseURL

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for invalid BaseURL %q, got nil", tc.baseURL)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "base_url") && !strings.Contains(strings.ToLower(err.Error()), "url") {
				t.Errorf("expected error message to mention base_url or url, got: %v", err)
			}
		})
	}
}

func TestValidate_Boundaries(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(c *Config)
		expectedErrSub string
	}{
		{
			name: "zero RequestTimeout",
			mutate: func(c *Config) {
				c.RequestTimeout = 0
			},
			expectedErrSub: "request_timeout",
		},
		{
			name: "negative RequestTimeout",
			mutate: func(c *Config) {
				c.RequestTimeout = -1 * time.Second
			},
			expectedErrSub: "request_timeout",
		},
		{
			name: "zero TTL",
			mutate: func(c *Config) {
				c.TTL = 0
			},
			expectedErrSub: "ttl",
		},
		{
			name: "negative TTL",
			mutate: func(c *Config) {
				c.TTL = -500 * time.Millisecond
			},
			expectedErrSub: "ttl",
		},
		{
			name: "zero MaxRetries",
			mutate: func(c *Config) {
				c.MaxRetries = 0
			},
			expectedErrSub: "max_retries",
		},
		{
			name: "negative MaxRetries",
			mutate: func(c *Config) {
				c.MaxRetries = -1
			},
			expectedErrSub: "max_retries",
		},
		{
			name: "zero BufferSize",
			mutate: func(c *Config) {
				c.BufferSize = 0
			},
			expectedErrSub: "buffer_size",
		},
		{
			name: "negative BufferSize",
			mutate: func(c *Config) {
				c.BufferSize = -10
			},
			expectedErrSub: "buffer_size",
		},
		{
			name: "zero RetryBackoff",
			mutate: func(c *Config) {
				c.RetryBackoff = 0
			},
			expectedErrSub: "retry_backoff",
		},
		{
			name: "negative RetryBackoff",
			mutate: func(c *Config) {
				c.RetryBackoff = -50 * time.Millisecond
			},
			expectedErrSub: "retry_backoff",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Token = "valid-token"
			tc.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error for boundary case %s, got nil", tc.name)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.expectedErrSub) {
				t.Errorf("expected error to contain %q, got: %v", tc.expectedErrSub, err)
			}
		})
	}
}
