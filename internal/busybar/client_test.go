package busybar

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
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

