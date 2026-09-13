package busybar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Host == "" {
		t.Errorf("expected default Host to be set, got empty string")
	}
	if cfg.Port != 80 {
		t.Errorf("expected default Port 80, got %d", cfg.Port)
	}
	if cfg.PingInterval <= 0 {
		t.Errorf("expected positive PingInterval, got %v", cfg.PingInterval)
	}
	if cfg.ReadTimeout <= 0 {
		t.Errorf("expected positive ReadTimeout, got %v", cfg.ReadTimeout)
	}
	if cfg.InitialBackoff <= 0 {
		t.Errorf("expected positive InitialBackoff, got %v", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff <= cfg.InitialBackoff {
		t.Errorf("expected MaxBackoff > InitialBackoff, got max %v, initial %v", cfg.MaxBackoff, cfg.InitialBackoff)
	}
	if cfg.BackoffMultiplier <= 1.0 {
		t.Errorf("expected BackoffMultiplier > 1.0, got %f", cfg.BackoffMultiplier)
	}
	if cfg.BufferSize <= 0 {
		t.Errorf("expected positive BufferSize, got %d", cfg.BufferSize)
	}
}

func TestNewClient_Defaults(t *testing.T) {
	client := NewClient(Config{})
	cfg := client.Config()
	defaults := DefaultConfig()

	if cfg.Host != defaults.Host {
		t.Errorf("expected Host %s, got %s", defaults.Host, cfg.Host)
	}
	if cfg.Port != defaults.Port {
		t.Errorf("expected Port %d, got %d", defaults.Port, cfg.Port)
	}
	if cfg.PingInterval != defaults.PingInterval {
		t.Errorf("expected PingInterval %v, got %v", defaults.PingInterval, cfg.PingInterval)
	}
	if cfg.ReadTimeout != defaults.ReadTimeout {
		t.Errorf("expected ReadTimeout %v, got %v", defaults.ReadTimeout, cfg.ReadTimeout)
	}
	if cfg.InitialBackoff != defaults.InitialBackoff {
		t.Errorf("expected InitialBackoff %v, got %v", defaults.InitialBackoff, cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != defaults.MaxBackoff {
		t.Errorf("expected MaxBackoff %v, got %v", defaults.MaxBackoff, cfg.MaxBackoff)
	}
	if cfg.BackoffMultiplier != defaults.BackoffMultiplier {
		t.Errorf("expected BackoffMultiplier %f, got %f", defaults.BackoffMultiplier, cfg.BackoffMultiplier)
	}
	if cfg.BufferSize != defaults.BufferSize {
		t.Errorf("expected BufferSize %d, got %d", defaults.BufferSize, cfg.BufferSize)
	}
	if client.Frames() == nil {
		t.Errorf("expected non-nil Frames channel")
	}
	if client.IsConnected() {
		t.Errorf("expected initial IsConnected to be false")
	}
}

func TestNewClient_CustomConfig(t *testing.T) {
	custom := Config{
		Host:              "10.0.0.50",
		Port:              8080,
		PingInterval:      5 * time.Second,
		ReadTimeout:       15 * time.Second,
		InitialBackoff:    2 * time.Second,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
		BufferSize:        50,
	}

	client := NewClient(custom)
	cfg := client.Config()

	if cfg.Host != custom.Host {
		t.Errorf("expected Host %s, got %s", custom.Host, cfg.Host)
	}
	if cfg.Port != custom.Port {
		t.Errorf("expected Port %d, got %d", custom.Port, cfg.Port)
	}
	if cfg.PingInterval != custom.PingInterval {
		t.Errorf("expected PingInterval %v, got %v", custom.PingInterval, cfg.PingInterval)
	}
	if cfg.ReadTimeout != custom.ReadTimeout {
		t.Errorf("expected ReadTimeout %v, got %v", custom.ReadTimeout, cfg.ReadTimeout)
	}
	if cfg.InitialBackoff != custom.InitialBackoff {
		t.Errorf("expected InitialBackoff %v, got %v", custom.InitialBackoff, cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != custom.MaxBackoff {
		t.Errorf("expected MaxBackoff %v, got %v", custom.MaxBackoff, cfg.MaxBackoff)
	}
	if cfg.BackoffMultiplier != custom.BackoffMultiplier {
		t.Errorf("expected BackoffMultiplier %f, got %f", custom.BackoffMultiplier, cfg.BackoffMultiplier)
	}
	if cfg.BufferSize != custom.BufferSize {
		t.Errorf("expected BufferSize %d, got %d", custom.BufferSize, cfg.BufferSize)
	}
}

func TestConnectionStatus_JSONSerialization(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	status := ConnectionStatus{
		Connected:          true,
		ReconnectCount:     5,
		LastConnectedAt:    now,
		LastDisconnectedAt: now.Add(-10 * time.Minute),
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal ConnectionStatus: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if parsed["connected"] != true {
		t.Errorf("expected connected=true, got %v", parsed["connected"])
	}
	if floatVal, ok := parsed["reconnect_count"].(float64); !ok || int64(floatVal) != 5 {
		t.Errorf("expected reconnect_count=5, got %v", parsed["reconnect_count"])
	}
	if parsed["last_connected_at"] == nil || parsed["last_connected_at"] == "" {
		t.Errorf("expected last_connected_at to be present in JSON")
	}
	if parsed["last_disconnected_at"] == nil || parsed["last_disconnected_at"] == "" {
		t.Errorf("expected last_disconnected_at to be present in JSON")
	}
}

func TestThreadSafeStatusAccess(t *testing.T) {
	client := NewClient(Config{})

	const iterations = 500
	var wg sync.WaitGroup

	// Reader goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = client.IsConnected()
				status := client.Status()
				_ = status.Connected
				_ = status.ReconnectCount
				_ = client.Frames()
			}
		}()
	}

	// Writer goroutines
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				now := time.Now()
				client.setStatus(ConnectionStatus{
					Connected:          j%2 == 0,
					ReconnectCount:     int64(j),
					LastConnectedAt:    now,
					LastDisconnectedAt: now.Add(-time.Second),
				})
			}
		}(i)
	}

	wg.Wait()
}

func TestCalculateBackoff_BoundariesAndJitter(t *testing.T) {
	client := NewClient(Config{
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     10 * time.Second,
	})

	current := 4 * time.Second
	maxBackoff := 10 * time.Second

	// Verify jitter stays within [0, current] when current < maxBackoff
	seenDifferent := false
	var firstVal time.Duration
	for i := 0; i < 100; i++ {
		b := CalculateBackoff(current, maxBackoff)
		if b < 0 || b > current {
			t.Fatalf("CalculateBackoff(%v, %v) = %v out of bounds [0, %v]", current, maxBackoff, b, current)
		}
		if i == 0 {
			firstVal = b
		} else if b != firstVal {
			seenDifferent = true
		}
	}
	if !seenDifferent {
		t.Errorf("expected jitter to produce varied backoff values, but all were identical: %v", firstVal)
	}

	// Verify jitter stays within [0, maxBackoff] when current > maxBackoff
	currentOverMax := 20 * time.Second
	for i := 0; i < 100; i++ {
		b := client.CalculateBackoff(currentOverMax)
		if b < 0 || b > maxBackoff {
			t.Fatalf("client.CalculateBackoff(%v) = %v out of bounds [0, %v]", currentOverMax, b, maxBackoff)
		}
	}

	// Edge case: zero or negative backoff
	if b := CalculateBackoff(0, maxBackoff); b != 0 {
		t.Errorf("expected 0 for zero current backoff, got %v", b)
	}
}

func TestBackoffProgression(t *testing.T) {
	cfg := Config{
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        5 * time.Second,
		BackoffMultiplier: 2.0,
	}
	client := NewClient(cfg)

	current := client.Config().InitialBackoff
	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		5 * time.Second, // Capped at MaxBackoff
		5 * time.Second,
	}

	for step, exp := range expected {
		if current != exp {
			t.Errorf("step %d: expected backoff %v, got %v", step, exp, current)
		}
		next := client.nextBackoff(current)
		current = next
	}
}

func TestReconnectionLoop_TelemetryAndLifecycle(t *testing.T) {
	client := NewClient(Config{
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	})

	// Override sleepFn to execute immediately without actual delay
	client.sleepFn = func(ctx context.Context, d time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	done := make(chan struct{})
	var once sync.Once
	dropCount := 0
	client.connectFn = func(ctx context.Context) error {
		client.mu.Lock()
		dropCount++
		client.status.Connected = true
		client.status.LastConnectedAt = time.Now()
		count := dropCount
		client.mu.Unlock()

		if count >= 3 {
			once.Do(func() { close(done) })
		}
		return errors.New("simulated connection drop")
	}

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	// Start again should error
	if err := client.Start(ctx); err == nil {
		t.Errorf("expected error on duplicate Start(), got nil")
	}

	// Wait for at least 3 connection drops
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for 3 reconnect attempts")
	}

	// Wait for Close
	if err := client.Close(); err != nil {
		t.Fatalf("failed to close client: %v", err)
	}

	status := client.Status()
	if status.Connected {
		t.Errorf("expected Connected=false after close, got true")
	}
	if status.ReconnectCount < 3 {
		t.Errorf("expected ReconnectCount >= 3, got %d", status.ReconnectCount)
	}
	if status.LastDisconnectedAt.IsZero() {
		t.Errorf("expected LastDisconnectedAt to be set, got zero time")
	}
}

func TestBackoffReset_After30SecondsUptime(t *testing.T) {
	client := NewClient(Config{
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
	})

	var recordedDelays []time.Duration
	var mu sync.Mutex
	done := make(chan struct{})
	var once sync.Once

	client.sleepFn = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		recordedDelays = append(recordedDelays, client.CurrentBackoff())
		count := len(recordedDelays)
		mu.Unlock()

		if count >= 3 {
			once.Do(func() { close(done) })
		}
		return nil
	}

	iteration := 0
	client.connectFn = func(ctx context.Context) error {
		iteration++
		client.mu.Lock()
		client.status.Connected = true
		switch iteration {
		case 1:
			// First drop after unstable connection (< 30s uptime, simulated 5s)
			client.status.LastConnectedAt = time.Now().Add(-5 * time.Second)
		case 2:
			// Second drop after another unstable connection (simulated 10s)
			client.status.LastConnectedAt = time.Now().Add(-10 * time.Second)
		case 3:
			// Third drop after STABLE connection (>= 30s uptime, simulated 35s)
			client.status.LastConnectedAt = time.Now().Add(-35 * time.Second)
		}
		client.mu.Unlock()
		return errors.New("drop")
	}

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for 3 backoff iterations")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("failed to close client: %v", err)
	}

	mu.Lock()
	delays := append([]time.Duration(nil), recordedDelays...)
	mu.Unlock()

	if len(delays) < 3 {
		t.Fatalf("expected at least 3 delay recordings, got %d", len(delays))
	}

	// Iteration 1: Initial backoff = 1s.
	if delays[0] != 1*time.Second {
		t.Errorf("expected initial backoff delay 1s, got %v", delays[0])
	}
	// Iteration 2: After unstable 5s connection, backoff progressed = 2s.
	if delays[1] != 2*time.Second {
		t.Errorf("expected progressed backoff delay 2s, got %v", delays[1])
	}
	// Iteration 3: After stable 35s connection (>= 30s), backoff delay MUST reset to InitialBackoff (1s).
	if delays[2] != 1*time.Second {
		t.Errorf("expected backoff delay after 35s stable uptime to reset to 1s, got %v", delays[2])
	}
}

func parseServerHostPort(t *testing.T, s *httptest.Server) (string, int) {
	t.Helper()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatalf("failed to parse server URL %q: %v", s.URL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("failed to parse port from %q: %v", u.Host, err)
	}
	return u.Hostname(), port
}

func TestWebSocketActivationHandshake(t *testing.T) {
	activationReceived := make(chan string, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status/ws" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("failed to accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusInternalError, "closed")

		typ, payload, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("failed to read activation frame: %v", err)
			return
		}
		if typ != websocket.MessageText {
			t.Errorf("expected MessageText, got %v", typ)
		}
		activationReceived <- string(payload)

		// Hold connection until context cancelled or closed
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	client := NewClient(Config{
		Host:           host,
		Port:           port,
		PingInterval:   100 * time.Millisecond,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	select {
	case msg := <-activationReceived:
		expectedMsg := `{"enable": true}`
		if msg != expectedMsg {
			t.Errorf("expected activation frame %q, got %q", expectedMsg, msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for stream activation frame")
	}

	// Verify telemetry state updated
	var connected bool
	for start := time.Now(); time.Since(start) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
		status := client.Status()
		if status.Connected && !status.LastConnectedAt.IsZero() {
			connected = true
			break
		}
	}
	if !connected {
		t.Errorf("expected client status Connected=true and non-zero LastConnectedAt")
	}
}

func TestWebSocketKeepalivePing(t *testing.T) {
	var pingCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
			OnPingReceived: func(ctx context.Context, payload []byte) bool {
				pingCount.Add(1)
				return true
			},
		})
		if err != nil {
			t.Errorf("accept error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusInternalError, "closed")

		// Read activation frame
		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}

		// Keep connection open and read
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	client := NewClient(Config{
		Host:           host,
		Port:           port,
		PingInterval:   30 * time.Millisecond,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	// Wait for multiple keepalive ping dispatches
	start := time.Now()
	for time.Since(start) < 2*time.Second {
		if pingCount.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if count := pingCount.Load(); count < 2 {
		t.Errorf("expected at least 2 ping frames dispatched, got %d", count)
	}
}

func TestWebSocketGracefulShutdown(t *testing.T) {
	closeStatusCode := make(chan websocket.StatusCode, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusInternalError, "closed")

		// Read activation message
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}

		// Read until closure
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				code := websocket.CloseStatus(err)
				closeStatusCode <- code
				return
			}
		}
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	client := NewClient(Config{
		Host:           host,
		Port:           port,
		PingInterval:   50 * time.Millisecond,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	// Wait until client reports connected
	for start := time.Now(); time.Since(start) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
		if client.IsConnected() {
			break
		}
	}
	if !client.IsConnected() {
		t.Fatal("client never reached connected state")
	}

	// Close client gracefully
	if err := client.Close(); err != nil {
		t.Fatalf("client.Close() failed: %v", err)
	}

	// Verify server received StatusNormalClosure
	select {
	case code := <-closeStatusCode:
		if code != websocket.StatusNormalClosure {
			t.Errorf("expected close code StatusNormalClosure (%d), got %v", websocket.StatusNormalClosure, code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for normal close handshake on server")
	}

	// Verify status after shutdown
	if client.IsConnected() {
		t.Errorf("expected client.IsConnected() == false after Close()")
	}
}

func TestWebSocketActivationFailure_ClosesAndRetries(t *testing.T) {
	var connCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := connCount.Add(1)
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		if count == 1 {
			// First attempt: immediately abort so client activation write fails or connection drops
			conn.Close(websocket.StatusInternalError, "simulated handshake error")
			return
		}
		// Subsequent attempts: accept and read normally
		defer conn.Close(websocket.StatusInternalError, "closed")
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	client := NewClient(Config{
		Host:           host,
		Port:           port,
		PingInterval:   100 * time.Millisecond,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     30 * time.Millisecond,
	})

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	// Wait until client retried and reconnected
	start := time.Now()
	for time.Since(start) < 3*time.Second {
		if connCount.Load() >= 2 && client.IsConnected() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if connCount.Load() < 2 {
		t.Errorf("expected client to reconnect after handshake failure, connection attempts: %d", connCount.Load())
	}
}

func TestWebSocketBinaryFrameForwarding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("failed to accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusInternalError, "closed")

		// Read activation handshake
		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}

		// Send binary frames
		testFrames := [][]byte{
			{0x01, 0x02, 0x03},
			{0x10, 0x20, 0x30, 0x40},
			{0xFF, 0xEE},
		}
		for _, frame := range testFrames {
			if err := conn.Write(r.Context(), websocket.MessageBinary, frame); err != nil {
				return
			}
		}

		// Keep connection open until closed
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	client := NewClient(Config{
		Host:           host,
		Port:           port,
		PingInterval:   1 * time.Second,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		BufferSize:     10,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	expectedFrames := [][]byte{
		{0x01, 0x02, 0x03},
		{0x10, 0x20, 0x30, 0x40},
		{0xFF, 0xEE},
	}

	for i, expected := range expectedFrames {
		select {
		case frame, ok := <-client.Frames():
			if !ok {
				t.Fatalf("frame channel closed unexpectedly at index %d", i)
			}
			if string(frame) != string(expected) {
				t.Errorf("frame %d: expected %v, got %v", i, expected, frame)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for binary frame %d", i)
		}
	}
}

func TestWebSocketSlowConsumer_NonBlocking(t *testing.T) {
	sendComplete := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusInternalError, "closed")

		// Read activation handshake
		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}

		// BufferSize is 2. Send 6 frames without the client consuming them.
		for i := 0; i < 6; i++ {
			if err := conn.Write(r.Context(), websocket.MessageBinary, []byte{byte(i)}); err != nil {
				return
			}
		}
		close(sendComplete)

		// Keep connection open until closed
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	client := NewClient(Config{
		Host:           host,
		Port:           port,
		PingInterval:   1 * time.Second,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		BufferSize:     2,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	// Wait for server to finish sending all 6 frames
	select {
	case <-sendComplete:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to send frames")
	}

	// Wait for client to process and drop overflow frames
	var overflow int64
	for start := time.Now(); time.Since(start) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
		overflow = client.OverflowCount()
		if overflow > 0 {
			break
		}
	}

	if overflow == 0 {
		t.Errorf("expected overflow count > 0, got 0")
	}

	// Verify buffer still holds exactly BufferSize (2) frames
	for i := 0; i < 2; i++ {
		select {
		case <-client.Frames():
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("expected buffered frame %d", i)
		}
	}
}

func TestWebSocketUnexpectedTextFrames_Discarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusInternalError, "closed")

		// Read activation handshake
		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}

		// Send unexpected text frames
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"status": "diagnostic"}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`plain text warning`))

		// Follow with a binary frame
		_ = conn.Write(r.Context(), websocket.MessageBinary, []byte{0xDE, 0xAD, 0xBE, 0xEF})

		// Keep connection open until closed
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	client := NewClient(Config{
		Host:           host,
		Port:           port,
		PingInterval:   1 * time.Second,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		BufferSize:     10,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer client.Close()

	// Only the binary frame should be delivered to Frames()
	select {
	case frame := <-client.Frames():
		expected := []byte{0xDE, 0xAD, 0xBE, 0xEF}
		if string(frame) != string(expected) {
			t.Errorf("expected frame %v, got %v", expected, frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for binary frame after text frames")
	}

	// Connection must remain alive
	if !client.IsConnected() {
		t.Errorf("expected client to remain connected after discarding text frames")
	}
}

func TestWebSocketGracefulShutdown_ClosesFramesChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusInternalError, "closed")

		// Read activation handshake
		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}

		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	host, port := parseServerHostPort(t, server)
	client := NewClient(Config{
		Host:           host,
		Port:           port,
		PingInterval:   1 * time.Second,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	// Wait for connected
	for start := time.Now(); time.Since(start) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
		if client.IsConnected() {
			break
		}
	}

	if err := client.Close(); err != nil {
		t.Fatalf("client.Close() failed: %v", err)
	}

	// Verify Frames channel is closed
	select {
	case _, ok := <-client.Frames():
		if ok {
			t.Errorf("expected Frames() channel to be closed, but received value")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for Frames() channel closure")
	}
}



