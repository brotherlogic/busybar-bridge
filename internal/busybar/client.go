package busybar

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const defaultStableConnectionThreshold = 30 * time.Second

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
	cfg             Config
	status          ConnectionStatus
	mu              sync.RWMutex
	framesChan      chan []byte
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	connectFn       func(ctx context.Context) error
	sleepFn         func(ctx context.Context, d time.Duration) error
	stableThreshold time.Duration
	backoffDelay    time.Duration
	activeConn      *websocket.Conn
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

// defaultSleep waits for the duration or context cancellation.
func defaultSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
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

	c := &Client{
		cfg:             cfg,
		framesChan:      make(chan []byte, cfg.BufferSize),
		stableThreshold: defaultStableConnectionThreshold,
		backoffDelay:    cfg.InitialBackoff,
	}
	c.connectFn = c.connectAndSupervise
	c.sleepFn = defaultSleep
	return c
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

// CurrentBackoff returns the current backoff delay before jitter.
func (c *Client) CurrentBackoff() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.backoffDelay
}

// CalculateBackoff computes exponential backoff with full jitter:
// sleep = rand.Float64() * min(maxBackoff, currentBackoff).
func CalculateBackoff(currentBackoff, maxBackoff time.Duration) time.Duration {
	effective := currentBackoff
	if maxBackoff > 0 && effective > maxBackoff {
		effective = maxBackoff
	}
	if effective <= 0 {
		return 0
	}
	return time.Duration(rand.Float64() * float64(effective))
}

// CalculateBackoff calculates full jitter sleep duration using the client's configured MaxBackoff.
func (c *Client) CalculateBackoff(currentBackoff time.Duration) time.Duration {
	c.mu.RLock()
	maxBackoff := c.cfg.MaxBackoff
	c.mu.RUnlock()
	return CalculateBackoff(currentBackoff, maxBackoff)
}

// nextBackoff computes the next exponential backoff delay capped at MaxBackoff.
func (c *Client) nextBackoff(currentBackoff time.Duration) time.Duration {
	c.mu.RLock()
	multiplier := c.cfg.BackoffMultiplier
	maxBackoff := c.cfg.MaxBackoff
	c.mu.RUnlock()

	next := time.Duration(float64(currentBackoff) * multiplier)
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}

// Start starts the background reconnection loop governed by the provided context.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		return errors.New("client already started")
	}

	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.run(runCtx)
	}()

	return nil
}

// Close gracefully stops the client and waits for all background goroutines to finish.
func (c *Client) Close() error {
	c.mu.Lock()
	cancel := c.cancel
	conn := c.activeConn
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "shutting down")
	}
	c.wg.Wait()
	return nil
}

// endpointURL formats the WebSocket endpoint URL based on client configuration.
func (c *Client) endpointURL() string {
	c.mu.RLock()
	host := c.cfg.Host
	port := c.cfg.Port
	c.mu.RUnlock()

	trimmed := host
	scheme := "ws"
	if strings.HasPrefix(trimmed, "http://") {
		trimmed = strings.TrimPrefix(trimmed, "http://")
	} else if strings.HasPrefix(trimmed, "https://") {
		trimmed = strings.TrimPrefix(trimmed, "https://")
		scheme = "wss"
	} else if strings.HasPrefix(trimmed, "ws://") {
		trimmed = strings.TrimPrefix(trimmed, "ws://")
	} else if strings.HasPrefix(trimmed, "wss://") {
		trimmed = strings.TrimPrefix(trimmed, "wss://")
		scheme = "wss"
	}

	trimmed = strings.TrimRight(trimmed, "/")
	if idx := strings.Index(trimmed, "/"); idx != -1 {
		trimmed = trimmed[:idx]
	}

	var hostPort string
	if h, p, err := net.SplitHostPort(trimmed); err == nil {
		if port > 0 {
			hostPort = net.JoinHostPort(h, strconv.Itoa(port))
		} else {
			hostPort = net.JoinHostPort(h, p)
		}
	} else {
		if port > 0 {
			hostPort = net.JoinHostPort(trimmed, strconv.Itoa(port))
		} else {
			hostPort = trimmed
		}
	}

	return fmt.Sprintf("%s://%s/api/status/ws", scheme, hostPort)
}

// run is the background reconnection loop with exponential backoff and full jitter.
func (c *Client) run(ctx context.Context) {
	c.mu.RLock()
	currentBackoff := c.cfg.InitialBackoff
	c.mu.RUnlock()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		connectStart := time.Now()

		// Attempt connection & supervision
		connectErr := c.connectFn(ctx)

		disconnectTime := time.Now()

		c.mu.Lock()
		wasConnected := c.status.Connected
		lastConnectedAt := c.status.LastConnectedAt
		c.status.Connected = false
		c.status.LastDisconnectedAt = disconnectTime
		c.status.ReconnectCount++
		threshold := c.stableThreshold
		c.mu.Unlock()

		if ctx.Err() != nil {
			return
		}

		if threshold <= 0 {
			threshold = defaultStableConnectionThreshold
		}

		// Backoff reset logic: reset to InitialBackoff only after the connection
		// has remained stably connected for at least 30 seconds.
		uptime := time.Duration(0)
		if wasConnected && !lastConnectedAt.IsZero() {
			uptime = disconnectTime.Sub(lastConnectedAt)
		} else if connectErr == nil {
			uptime = disconnectTime.Sub(connectStart)
		}

		c.mu.RLock()
		initialBackoff := c.cfg.InitialBackoff
		c.mu.RUnlock()

		if uptime >= threshold {
			currentBackoff = initialBackoff
		}

		c.mu.Lock()
		c.backoffDelay = currentBackoff
		c.mu.Unlock()

		sleepDuration := c.CalculateBackoff(currentBackoff)
		currentBackoff = c.nextBackoff(currentBackoff)

		if err := c.sleepFn(ctx, sleepDuration); err != nil {
			return
		}
	}
}

// connectAndSupervise dials the WebSocket endpoint, conducts stream activation handshake,
// runs the keepalive ping ticker loop, and supervises the connection stream.
func (c *Client) connectAndSupervise(ctx context.Context) error {
	endpoint := c.endpointURL()

	c.mu.RLock()
	dialTimeout := c.cfg.ReadTimeout
	pingInterval := c.cfg.PingInterval
	c.mu.RUnlock()

	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	conn, _, err := websocket.Dial(dialCtx, endpoint, nil)
	dialCancel()
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusInternalError, "connection closed")

	if ctx.Err() != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "shutting down")
		return ctx.Err()
	}

	c.mu.Lock()
	c.activeConn = conn
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.activeConn == conn {
			c.activeConn = nil
		}
		c.mu.Unlock()
	}()

	// Send stream activation message {"enable": true} as websocket.MessageText
	writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
	err = conn.Write(writeCtx, websocket.MessageText, []byte(`{"enable": true}`))
	writeCancel()
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "activation write failed")
		return err
	}

	// Update telemetry state on connection establishment
	c.mu.Lock()
	c.status.Connected = true
	c.status.LastConnectedAt = time.Now()
	c.mu.Unlock()

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	// Start keepalive ping ticker in a child goroutine periodically sending conn.Ping(ctx) every PingInterval
	var pingWg sync.WaitGroup
	pingWg.Add(1)
	go func() {
		defer pingWg.Done()
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				if err := conn.Ping(connCtx); err != nil {
					connCancel()
					return
				}
			}
		}
	}()
	defer pingWg.Wait()

	// Read loop supervision
	err = c.readLoop(connCtx, conn)
	connCancel()
	return err
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			return err
		}
	}
}

