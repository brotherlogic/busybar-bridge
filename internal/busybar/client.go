package busybar

import (
	"context"
	"sync"
	"time"
)

// Config encapsulates connection, backoff, and buffering parameters for the Busy Bar client.
type Config struct {
	Host              string
	Port              int
	PingInterval      time.Duration
	ReadTimeout       time.Duration
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
	BufferSize        int
}

// ConnectionStatus reports connection and telemetry metadata with JSON tags.
type ConnectionStatus struct {
	Connected          bool      `json:"connected"`
	ReconnectCount     int64     `json:"reconnect_count"`
	LastConnectedAt    time.Time `json:"last_connected_at,omitempty"`
	LastDisconnectedAt time.Time `json:"last_disconnected_at,omitempty"`
}

// Client manages the lifecycle, ingestion, and status accessors for a Busy Bar device stream.
type Client struct {
	cfg        Config
	status     ConnectionStatus
	mu         sync.RWMutex
	framesChan chan []byte
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// DefaultConfig returns sensible production defaults for the Busy Bar client.
func DefaultConfig() Config {
	return Config{
		Host:              "192.168.68.196",
		Port:              80,
		PingInterval:      10 * time.Second,
		ReadTimeout:       30 * time.Second,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        60 * time.Second,
		BackoffMultiplier: 1.5,
		BufferSize:        100,
	}
}

// NewClient initializes a new Busy Bar Client with default fallbacks for unconfigured fields.
func NewClient(cfg Config) *Client {
	defaults := DefaultConfig()
	if cfg.Host == "" {
		cfg.Host = defaults.Host
	}
	if cfg.Port <= 0 {
		cfg.Port = defaults.Port
	}
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = defaults.PingInterval
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = defaults.ReadTimeout
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = defaults.InitialBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = defaults.MaxBackoff
	}
	if cfg.BackoffMultiplier <= 0 {
		cfg.BackoffMultiplier = defaults.BackoffMultiplier
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = defaults.BufferSize
	}

	return &Client{
		cfg:        cfg,
		framesChan: make(chan []byte, cfg.BufferSize),
	}
}

// Config returns a copy of the client configuration.
func (c *Client) Config() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

// Frames returns a receive-only channel for ingested binary frames.
func (c *Client) Frames() <-chan []byte {
	return c.framesChan
}

// Status returns a point-in-time copy of the connection status.
func (c *Client) Status() ConnectionStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// IsConnected returns whether the client is currently connected.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status.Connected
}

// setStatus updates the connection status under write lock.
func (c *Client) setStatus(status ConnectionStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = status
}
