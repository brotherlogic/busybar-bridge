package busybar

import (
	"encoding/json"
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
