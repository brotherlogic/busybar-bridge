package hass

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrMissingToken indicates the bearer token is missing or empty.
	ErrMissingToken = errors.New("token is required")
	// ErrInvalidBaseURL indicates the base_url is malformed or does not use http/https with a host.
	ErrInvalidBaseURL = errors.New("invalid base_url: must be a valid http or https URL")
	// ErrInvalidRequestTimeout indicates request_timeout is not positive.
	ErrInvalidRequestTimeout = errors.New("request_timeout must be positive")
	// ErrInvalidTTL indicates ttl is not positive.
	ErrInvalidTTL = errors.New("ttl must be positive")
	// ErrInvalidMaxRetries indicates max_retries is not positive.
	ErrInvalidMaxRetries = errors.New("max_retries must be positive")
	// ErrInvalidBufferSize indicates buffer_size is not positive.
	ErrInvalidBufferSize = errors.New("buffer_size must be positive")
	// ErrInvalidRetryBackoff indicates retry_backoff is not positive.
	ErrInvalidRetryBackoff = errors.New("retry_backoff must be positive")
)

// Config defines configuration parameters for the Home Assistant REST client.
type Config struct {
	BaseURL        string
	Token          string
	EventType      string
	RequestTimeout time.Duration
	TTL            time.Duration
	MaxRetries     int
	BufferSize     int
	RetryBackoff   time.Duration
}

// DefaultConfig returns default configuration parameters.
func DefaultConfig() Config {
	return Config{
		BaseURL:        "http://homeassistant.local:8123",
		EventType:      "busybar_event",
		RequestTimeout: 2 * time.Second,
		TTL:            2500 * time.Millisecond,
		MaxRetries:     2,
		BufferSize:     100,
		RetryBackoff:   100 * time.Millisecond,
	}
}

// Validate verifies that the configuration invariants are satisfied.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Token) == "" {
		return ErrMissingToken
	}

	if strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("%w: empty URL", ErrInvalidBaseURL)
	}

	u, err := url.ParseRequestURI(c.BaseURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBaseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q not supported", ErrInvalidBaseURL, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: host is empty", ErrInvalidBaseURL)
	}

	if c.RequestTimeout <= 0 {
		return ErrInvalidRequestTimeout
	}
	if c.TTL <= 0 {
		return ErrInvalidTTL
	}
	if c.MaxRetries <= 0 {
		return ErrInvalidMaxRetries
	}
	if c.BufferSize <= 0 {
		return ErrInvalidBufferSize
	}
	if c.RetryBackoff <= 0 {
		return ErrInvalidRetryBackoff
	}

	return nil
}
