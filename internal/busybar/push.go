package busybar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb_busybar "github.com/brotherlogic/busybar-bridge/pkg/pb/busybar"
	"google.golang.org/protobuf/proto"
)

// Aliases for upstream Protobuf Frame and Screen types.
type Frame = pb_busybar.Frame
type Screen = pb_busybar.Screen

const (
	ScreenFront = pb_busybar.Screen_FRONT
	ScreenBack  = pb_busybar.Screen_BACK
)

var (
	// ErrNilFrame is returned when a nil frame pointer is pushed.
	ErrNilFrame = errors.New("frame cannot be nil")
	// ErrEmptyData is returned when a frame with empty payload data is pushed.
	ErrEmptyData = errors.New("frame data cannot be empty")
	// ErrClientClosed is returned when attempting to push to a closed client.
	ErrClientClosed = errors.New("push client is closed")
)

// PushConfig encapsulates connection, timeout, and authentication settings for the outbound push client.
type PushConfig struct {
	Enabled       bool
	Host          string
	Port          int
	APIKey        string
	Timeout       time.Duration
	DefaultScreen Screen
}

// DefaultPushConfig returns sensible defaults for outbound push client configuration.
func DefaultPushConfig() PushConfig {
	return PushConfig{
		Enabled:       false,
		Host:          "192.168.68.196",
		Port:          80,
		APIKey:        "",
		Timeout:       3 * time.Second,
		DefaultScreen: ScreenFront,
	}
}

// PushTelemetryRecorder defines telemetry instrumentation methods for outbound push operations.
type PushTelemetryRecorder interface {
	RecordPushAttempt()
	RecordPushResult(success bool, latency time.Duration, err error)
	RecordPushDrop()
}

// NoopPushTelemetryRecorder provides a no-op implementation of PushTelemetryRecorder.
type NoopPushTelemetryRecorder struct{}

// RecordPushAttempt is a no-op telemetry attempt recording.
func (NoopPushTelemetryRecorder) RecordPushAttempt() {}

// RecordPushResult is a no-op telemetry result recording.
func (NoopPushTelemetryRecorder) RecordPushResult(bool, time.Duration, error) {}

// RecordPushDrop is a no-op telemetry drop recording.
func (NoopPushTelemetryRecorder) RecordPushDrop() {}

// PushClient defines the interface for an in-process BusyBar outbound push client.
type PushClient interface {
	PushFrame(ctx context.Context, frame *Frame) error
	Start(ctx context.Context) error
	Close() error
	Config() PushConfig
}

type pushClient struct {
	cfg        PushConfig
	recorder   PushTelemetryRecorder
	mailbox    chan *Frame
	httpClient *http.Client
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	closed     atomic.Bool
	started    atomic.Bool
	closeOnce  sync.Once
	mailboxMu  sync.Mutex
}

// NewPushClient creates a new PushClient with the provided configuration and telemetry recorder.
func NewPushClient(cfg PushConfig, recorder PushTelemetryRecorder) PushClient {
	if recorder == nil {
		recorder = &NoopPushTelemetryRecorder{}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	return &pushClient{
		cfg:        cfg,
		recorder:   recorder,
		mailbox:    make(chan *Frame, 1),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Config returns the configuration used by this PushClient.
func (c *pushClient) Config() PushConfig {
	return c.cfg
}

// Start initializes the background worker and begins processing frames from the mailbox.
func (c *pushClient) Start(ctx context.Context) error {
	if c.started.Swap(true) {
		return errors.New("push client already started")
	}

	workerCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.wg.Add(1)
	go c.worker(workerCtx)

	return nil
}

// PushFrame enqueues a frame into the single-slot mailbox buffer with non-blocking overwrite semantics.
func (c *pushClient) PushFrame(ctx context.Context, frame *Frame) error {
	if !c.cfg.Enabled {
		return nil
	}
	if frame == nil {
		return ErrNilFrame
	}
	if len(frame.GetData()) == 0 {
		return ErrEmptyData
	}

	c.mailboxMu.Lock()
	defer c.mailboxMu.Unlock()

	if c.closed.Load() {
		return ErrClientClosed
	}

	select {
	case c.mailbox <- frame:
	default:
		// Slot is full: evict older frame, record drop, and insert the latest frame.
		select {
		case <-c.mailbox:
			c.recorder.RecordPushDrop()
		default:
		}
		c.mailbox <- frame
	}

	return nil
}

// Close gracefully terminates the background worker and shuts down the client.
func (c *pushClient) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		if c.cancel != nil {
			c.cancel()
		}
		c.wg.Wait()
	})
	return nil
}

func (c *pushClient) worker(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-c.mailbox:
			c.transmit(ctx, frame)
		}
	}
}

func (c *pushClient) transmit(ctx context.Context, frame *Frame) {
	if !c.cfg.Enabled {
		return
	}

	c.recorder.RecordPushAttempt()
	start := time.Now()

	// Clone frame so screen default logic does not mutate caller's original frame.
	sendFrame := proto.Clone(frame).(*pb_busybar.Frame)
	if sendFrame.Screen == 0 {
		if c.cfg.DefaultScreen != 0 {
			sendFrame.Screen = c.cfg.DefaultScreen
		} else {
			sendFrame.Screen = pb_busybar.Screen_FRONT
		}
	}

	payload, err := proto.Marshal(sendFrame)
	if err != nil {
		latency := time.Since(start)
		c.recorder.RecordPushResult(false, latency, fmt.Errorf("failed to marshal frame: %w", err))
		return
	}

	port := c.cfg.Port
	if port <= 0 {
		port = 80
	}
	host := strings.TrimPrefix(strings.TrimPrefix(c.cfg.Host, "http://"), "https://")
	hostPort := net.JoinHostPort(host, strconv.Itoa(port))
	targetURL := fmt.Sprintf("http://%s/api/display/frame", hostPort)

	timeout := c.cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		latency := time.Since(start)
		c.recorder.RecordPushResult(false, latency, err)
		return
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		c.recorder.RecordPushResult(false, latency, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		c.recorder.RecordPushResult(true, latency, nil)
	} else {
		c.recorder.RecordPushResult(false, latency, fmt.Errorf("HTTP push failed with status %d", resp.StatusCode))
	}
}
