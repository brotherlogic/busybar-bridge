package busybar_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/busybar"
	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
	pb_busybar "github.com/brotherlogic/busybar-bridge/pkg/pb/busybar"
	"google.golang.org/protobuf/proto"
)

func parseServerHostPort(t *testing.T, s *httptest.Server) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(s.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to split server host/port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("failed to parse server port: %v", err)
	}
	return host, port
}

func TestPushClient_DisabledFlag(t *testing.T) {
	var requestCount atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host, port := parseServerHostPort(t, ts)
	store := telemetry.NewStore()

	cfg := busybar.PushConfig{
		Enabled: false,
		Host:    host,
		Port:    port,
		Timeout: time.Second,
	}

	client := busybar.NewPushClient(cfg, store)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	frame := &busybar.Frame{
		Data: []byte("test-payload-bytes"),
	}

	if err := client.PushFrame(ctx, frame); err != nil {
		t.Fatalf("PushFrame failed on disabled client: %v", err)
	}

	// Wait briefly to confirm no background worker transmits
	time.Sleep(100 * time.Millisecond)

	if got := requestCount.Load(); got != 0 {
		t.Fatalf("expected 0 HTTP requests when disabled, got %d", got)
	}

	snap := store.Snapshot()
	if snap.Push.TotalAttempts != 0 {
		t.Fatalf("expected 0 push attempts in telemetry, got %d", snap.Push.TotalAttempts)
	}
}

func TestPushClient_Validation(t *testing.T) {
	var requestCount atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host, port := parseServerHostPort(t, ts)
	store := telemetry.NewStore()

	cfg := busybar.PushConfig{
		Enabled: true,
		Host:    host,
		Port:    port,
		Timeout: time.Second,
	}

	client := busybar.NewPushClient(cfg, store)
	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	// 1. Nil frame
	if err := client.PushFrame(ctx, nil); err == nil {
		t.Fatal("expected error when pushing nil frame, got nil")
	}

	// 2. Empty data bytes
	emptyFrame := &busybar.Frame{
		Data: []byte{},
	}
	if err := client.PushFrame(ctx, emptyFrame); err == nil {
		t.Fatal("expected error when pushing frame with empty data, got nil")
	}

	nilDataframe := &busybar.Frame{
		Data: nil,
	}
	if err := client.PushFrame(ctx, nilDataframe); err == nil {
		t.Fatal("expected error when pushing frame with nil data, got nil")
	}

	time.Sleep(50 * time.Millisecond)
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("expected 0 HTTP requests on validation failure, got %d", got)
	}
}

func TestPushClient_SuccessfulFramePush(t *testing.T) {
	var receivedMethod, receivedPath, receivedContentType, receivedAuth string
	var receivedBody []byte
	var mu sync.Mutex
	done := make(chan struct{}, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedContentType = r.Header.Get("Content-Type")
		receivedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
		select {
		case done <- struct{}{}:
		default:
		}
	}))
	defer ts.Close()

	host, port := parseServerHostPort(t, ts)
	store := telemetry.NewStore()

	cfg := busybar.PushConfig{
		Enabled:       true,
		Host:          host,
		Port:          port,
		APIKey:        "my-secret-key",
		Timeout:       2 * time.Second,
		DefaultScreen: busybar.ScreenFront,
	}

	client := busybar.NewPushClient(cfg, store)
	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	payloadBytes := []byte("RGB888_PIXEL_DATA_12345")
	frame := &busybar.Frame{
		Width:       64,
		Height:      32,
		Data:        payloadBytes,
		PixelFormat: pb_busybar.PixelFormat_RGB888,
	}

	if err := client.PushFrame(ctx, frame); err != nil {
		t.Fatalf("PushFrame failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for HTTP request to be delivered")
	}

	mu.Lock()
	defer mu.Unlock()

	if receivedMethod != http.MethodPost {
		t.Errorf("expected POST method, got %s", receivedMethod)
	}
	if receivedPath != "/api/display/frame" {
		t.Errorf("expected path /api/display/frame, got %s", receivedPath)
	}
	if receivedContentType != "application/octet-stream" {
		t.Errorf("expected Content-Type application/octet-stream, got %s", receivedContentType)
	}
	if receivedAuth != "Bearer my-secret-key" {
		t.Errorf("expected Authorization 'Bearer my-secret-key', got '%s'", receivedAuth)
	}

	var parsed pb_busybar.Frame
	if err := proto.Unmarshal(receivedBody, &parsed); err != nil {
		t.Fatalf("failed to unmarshal request body as Protobuf Frame: %v", err)
	}

	if string(parsed.GetData()) != string(payloadBytes) {
		t.Errorf("payload mismatch: expected %q, got %q", payloadBytes, parsed.GetData())
	}
	if parsed.GetScreen() != pb_busybar.Screen_FRONT {
		t.Errorf("expected default unset screen to be Screen_FRONT, got %v", parsed.GetScreen())
	}

	// Verify telemetry
	var snap telemetry.Snapshot
	for i := 0; i < 50; i++ {
		snap = store.Snapshot()
		if snap.Push.TotalSuccesses >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if snap.Push.TotalAttempts != 1 {
		t.Errorf("expected 1 push attempt, got %d", snap.Push.TotalAttempts)
	}
	if snap.Push.TotalSuccesses != 1 {
		t.Errorf("expected 1 push success, got %d", snap.Push.TotalSuccesses)
	}
	if snap.Push.TotalFailures != 0 {
		t.Errorf("expected 0 push failures, got %d", snap.Push.TotalFailures)
	}
	if snap.Push.TotalDropped != 0 {
		t.Errorf("expected 0 dropped frames, got %d", snap.Push.TotalDropped)
	}
	if snap.Push.LastError != "" {
		t.Errorf("expected empty LastError, got %q", snap.Push.LastError)
	}
}

func TestPushClient_MailboxOverwrite(t *testing.T) {
	holdServer := make(chan struct{})
	serverDone := make(chan struct{}, 10)
	var processedFrames atomic.Int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		processedFrames.Add(1)
		// Block on the first frame to keep the transmitter busy
		<-holdServer
		w.WriteHeader(http.StatusOK)
		serverDone <- struct{}{}
	}))
	defer ts.Close()

	host, port := parseServerHostPort(t, ts)
	store := telemetry.NewStore()

	cfg := busybar.PushConfig{
		Enabled: true,
		Host:    host,
		Port:    port,
		Timeout: 2 * time.Second,
	}

	client := busybar.NewPushClient(cfg, store)
	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	// Push first frame which will be picked up by the worker and block in handler
	if err := client.PushFrame(ctx, &busybar.Frame{Data: []byte("frame-1")}); err != nil {
		t.Fatalf("PushFrame 1 failed: %v", err)
	}

	// Wait until handler is handling frame 1
	for i := 0; i < 50; i++ {
		if processedFrames.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Now mailbox slot is free or can hold 1 frame. Rapidly push 5 frames.
	for i := 2; i <= 6; i++ {
		if err := client.PushFrame(ctx, &busybar.Frame{Data: []byte("frame-" + strconv.Itoa(i))}); err != nil {
			t.Fatalf("PushFrame %d failed: %v", i, err)
		}
	}

	// Stale frames must have been evicted and recorded in drops
	snap := store.Snapshot()
	if snap.Push.TotalDropped == 0 {
		t.Fatal("expected dropped frames to be > 0 due to mailbox overwrite")
	}

	// Release blocked server handler
	close(holdServer)

	// Wait for processing to settle
	time.Sleep(200 * time.Millisecond)
}

func TestPushClient_HTTPErrorHandling(t *testing.T) {
	t.Run("HTTP 500 Internal Server Error", func(t *testing.T) {
		done := make(chan struct{}, 1)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			select {
			case done <- struct{}{}:
			default:
			}
		}))
		defer ts.Close()

		host, port := parseServerHostPort(t, ts)
		store := telemetry.NewStore()

		cfg := busybar.PushConfig{
			Enabled: true,
			Host:    host,
			Port:    port,
			Timeout: time.Second,
		}

		client := busybar.NewPushClient(cfg, store)
		ctx := context.Background()
		if err := client.Start(ctx); err != nil {
			t.Fatalf("failed to start client: %v", err)
		}
		defer client.Close()

		if err := client.PushFrame(ctx, &busybar.Frame{Data: []byte("payload")}); err != nil {
			t.Fatalf("PushFrame failed: %v", err)
		}

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for HTTP request")
		}

		time.Sleep(50 * time.Millisecond)

		snap := store.Snapshot()
		if snap.Push.TotalFailures != 1 {
			t.Errorf("expected 1 push failure, got %d", snap.Push.TotalFailures)
		}
		if snap.Push.TotalSuccesses != 0 {
			t.Errorf("expected 0 push successes, got %d", snap.Push.TotalSuccesses)
		}
		if snap.Push.LastError == "" {
			t.Error("expected non-empty LastError on HTTP 500")
		}
	})

	t.Run("Connection Refused", func(t *testing.T) {
		// Pick an unused local port
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create listener: %v", err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close() // Close immediately so connection will be refused

		store := telemetry.NewStore()
		cfg := busybar.PushConfig{
			Enabled: true,
			Host:    "127.0.0.1",
			Port:    port,
			Timeout: 500 * time.Millisecond,
		}

		client := busybar.NewPushClient(cfg, store)
		ctx := context.Background()
		if err := client.Start(ctx); err != nil {
			t.Fatalf("failed to start client: %v", err)
		}
		defer client.Close()

		if err := client.PushFrame(ctx, &busybar.Frame{Data: []byte("payload")}); err != nil {
			t.Fatalf("PushFrame failed: %v", err)
		}

		time.Sleep(200 * time.Millisecond)

		snap := store.Snapshot()
		if snap.Push.TotalFailures != 1 {
			t.Errorf("expected 1 push failure on refused connection, got %d", snap.Push.TotalFailures)
		}
		if snap.Push.LastError == "" {
			t.Error("expected non-empty LastError on refused connection")
		}
	})

	t.Run("Timeout Handling", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		host, port := parseServerHostPort(t, ts)
		store := telemetry.NewStore()

		cfg := busybar.PushConfig{
			Enabled: true,
			Host:    host,
			Port:    port,
			Timeout: 50 * time.Millisecond, // shorter than server sleep
		}

		client := busybar.NewPushClient(cfg, store)
		ctx := context.Background()
		if err := client.Start(ctx); err != nil {
			t.Fatalf("failed to start client: %v", err)
		}
		defer client.Close()

		if err := client.PushFrame(ctx, &busybar.Frame{Data: []byte("payload")}); err != nil {
			t.Fatalf("PushFrame failed: %v", err)
		}

		time.Sleep(250 * time.Millisecond)

		snap := store.Snapshot()
		if snap.Push.TotalFailures != 1 {
			t.Errorf("expected 1 failure on timeout, got %d", snap.Push.TotalFailures)
		}
	})
}

func TestPushClient_WorkerShutdown(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host, port := parseServerHostPort(t, ts)
	store := telemetry.NewStore()

	cfg := busybar.PushConfig{
		Enabled: true,
		Host:    host,
		Port:    port,
		Timeout: time.Second,
	}

	client := busybar.NewPushClient(cfg, store)
	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	// Close client
	if err := client.Close(); err != nil {
		t.Fatalf("failed to close client: %v", err)
	}

	// PushFrame after close should fail
	if err := client.PushFrame(ctx, &busybar.Frame{Data: []byte("data")}); err == nil {
		t.Error("expected error when pushing to closed client, got nil")
	}

	// Calling Close() again should be idempotent and return nil
	if err := client.Close(); err != nil {
		t.Errorf("expected nil on second Close(), got: %v", err)
	}
}
