package hass

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
)

var (
	// ErrAlreadyStarted indicates Start was called on an already started Client.
	ErrAlreadyStarted = errors.New("client already started")
	// ErrAlreadyClosed indicates an operation was attempted on a closed Client.
	ErrAlreadyClosed = errors.New("client already closed")
)

// Dispatcher abstracts Home Assistant event ingestion and delivery lifecycle.
type Dispatcher interface {
	Start(ctx context.Context) error
	Enqueue(event Event) bool
	Close() error
}

// Client coordinates the bounded in-memory event queue and Home Assistant REST dispatcher.
type Client struct {
	cfg           Config
	eventsChan    chan Event
	store         *telemetry.Store
	httpClient    *http.Client
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	mu            sync.RWMutex
	started       bool
	closed        bool
	overflowDrops atomic.Int64
}

// Ensure *Client implements Dispatcher.
var _ Dispatcher = (*Client)(nil)

// NewClient validates the configuration, configures an HTTP client with connection pooling,
// and initializes a bounded FIFO event queue.
func NewClient(cfg Config, store *telemetry.Store) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   cfg.RequestTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   cfg.RequestTimeout,
	}

	return &Client{
		cfg:        cfg,
		eventsChan: make(chan Event, cfg.BufferSize),
		store:      store,
		httpClient: httpClient,
	}, nil
}

// Config returns a copy of the client configuration.
func (c *Client) Config() Config {
	return c.cfg
}

// Store returns the associated telemetry store.
func (c *Client) Store() *telemetry.Store {
	return c.store
}

// HTTPClient returns the configured HTTP client.
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

// EventsChan returns the receive-only event channel.
func (c *Client) EventsChan() <-chan Event {
	return c.eventsChan
}

// OverflowDrops returns the number of events dropped due to buffer overflow.
func (c *Client) OverflowDrops() int64 {
	return c.overflowDrops.Load()
}

// IsStarted returns whether the client background worker has started.
func (c *Client) IsStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

// IsClosed returns whether the client has been closed.
func (c *Client) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// Start launches the background worker goroutine for event processing.
// Returns ErrAlreadyStarted if the client was previously started.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return ErrAlreadyStarted
	}
	if c.closed {
		return ErrAlreadyClosed
	}
	c.started = true

	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.worker(runCtx)
	}()

	return nil
}

// Enqueue attempts a non-blocking send to the internal bounded queue.
// If the buffer is full or the client is closed, the event is dropped,
// drop telemetry is updated, and false is returned.
func (c *Client) Enqueue(event Event) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		c.overflowDrops.Add(1)
		if c.store != nil {
			c.store.RecordDrop("buffer_overflow")
		}
		return false
	}

	select {
	case c.eventsChan <- event:
		return true
	default:
		c.overflowDrops.Add(1)
		if c.store != nil {
			c.store.RecordDrop("buffer_overflow")
		}
		return false
	}
}

// Close cancels the worker context, closes the bounded channel, and waits
// for worker completion via sync.WaitGroup.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true

	if c.cancel != nil {
		c.cancel()
	}
	close(c.eventsChan)
	c.mu.Unlock()

	c.wg.Wait()
	return nil
}

// worker continuously reads from eventsChan until the context is canceled or channel is closed.
func (c *Client) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-c.eventsChan:
			if !ok {
				return
			}
		}
	}
}
