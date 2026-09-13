package hass_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/hass"
	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
)

func TestClientLifecycle(t *testing.T) {
	cfg := hass.DefaultConfig()
	cfg.Token = "test-bearer-token"
	store := telemetry.NewStore()

	// 1. Verify invalid config returns error
	invalidCfg := cfg
	invalidCfg.Token = ""
	if _, err := hass.NewClient(invalidCfg, store); err == nil {
		t.Fatalf("expected error creating client with invalid config, got nil")
	}

	// 2. Verify successful initialization
	client, err := hass.NewClient(cfg, store)
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	if client == nil {
		t.Fatalf("expected non-nil client")
	}

	// Verify interface compliance
	var _ hass.Dispatcher = client

	// 3. Verify successful start
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	// 4. Verify duplicate start returns error
	if err := client.Start(ctx); err == nil {
		t.Fatalf("expected error on duplicate Start(), got nil")
	} else if !errors.Is(err, hass.ErrAlreadyStarted) {
		t.Logf("got error on duplicate start: %v", err)
	}

	// 5. Verify Enqueue works when started
	ev := hass.NewButtonEvent("busybar", "ok", "press", time.Now().Unix())
	if ok := client.Enqueue(ev); !ok {
		t.Fatalf("expected Enqueue to return true, got false")
	}

	// 6. Verify clean close
	if err := client.Close(); err != nil {
		t.Fatalf("failed to close client: %v", err)
	}

	// 7. Verify idempotent Close
	if err := client.Close(); err != nil {
		t.Fatalf("expected second Close() to succeed idempotently, got %v", err)
	}

	// 8. Verify Enqueue returns false when closed
	if ok := client.Enqueue(ev); ok {
		t.Fatalf("expected Enqueue to return false after Close(), got true")
	}
}

func TestNonBlockingQueueOverflow(t *testing.T) {
	cfg := hass.DefaultConfig()
	cfg.Token = "test-bearer-token"
	cfg.BufferSize = 3 // small bounded queue
	store := telemetry.NewStore()

	client, err := hass.NewClient(cfg, store)
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	// Without calling Start(), the background worker is not draining eventsChan.
	// Fill the bounded buffer to exact capacity (3 events).
	for i := 0; i < 3; i++ {
		ev := hass.NewButtonEvent("busybar", "ok", "press", int64(100+i))
		if ok := client.Enqueue(ev); !ok {
			t.Fatalf("expected event %d to be enqueued successfully", i)
		}
	}

	if client.OverflowDrops() != 0 {
		t.Fatalf("expected 0 drops before buffer saturation, got %d", client.OverflowDrops())
	}
	if store.DropCount() != 0 {
		t.Fatalf("expected 0 store drops before saturation, got %d", store.DropCount())
	}

	// 4th and 5th enqueues should immediately drop without blocking
	done := make(chan bool, 1)
	go func() {
		ev4 := hass.NewButtonEvent("busybar", "ok", "press", 104)
		ok4 := client.Enqueue(ev4)
		if ok4 {
			t.Errorf("expected 4th event enqueue to return false on full queue, got true")
		}

		ev5 := hass.NewSwitchEvent("busybar", "busy", 105)
		ok5 := client.Enqueue(ev5)
		if ok5 {
			t.Errorf("expected 5th event enqueue to return false on full queue, got true")
		}
		done <- true
	}()

	select {
	case <-done:
		// Succeeded promptly without blocking
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Enqueue blocked when queue was full instead of returning immediately")
	}

	// Verify drop metrics
	if client.OverflowDrops() != 2 {
		t.Fatalf("expected 2 client overflow drops, got %d", client.OverflowDrops())
	}
	if store.DropCount() != 2 {
		t.Fatalf("expected 2 store drops, got %d", store.DropCount())
	}

	_ = client.Close()
}

func TestGracefulShutdown(t *testing.T) {
	cfg := hass.DefaultConfig()
	cfg.Token = "test-bearer-token"
	cfg.BufferSize = 100
	store := telemetry.NewStore()

	client, err := hass.NewClient(cfg, store)
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	// Enqueue a batch of events
	for i := 0; i < 20; i++ {
		ev := hass.NewButtonEvent("busybar", "ok", "press", int64(100+i))
		client.Enqueue(ev)
	}

	// Trigger Close() and ensure it terminates cleanly without leaking goroutines or hanging
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- client.Close()
	}()

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("expected clean Close(), got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Close() timed out waiting for worker to finish")
	}

	// Verify client is marked closed
	if !client.IsClosed() {
		t.Fatalf("expected client to report IsClosed() == true")
	}
}

func TestConcurrentEnqueueAndClose(t *testing.T) {
	cfg := hass.DefaultConfig()
	cfg.Token = "test-bearer-token"
	cfg.BufferSize = 50
	store := telemetry.NewStore()

	client, err := hass.NewClient(cfg, store)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var dropCount atomic.Int64

	numGoroutines := 10
	eventsPerGoroutine := 100

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				ev := hass.NewButtonEvent("busybar", "start", "press", int64(i))
				if client.Enqueue(ev) {
					successCount.Add(1)
				} else {
					dropCount.Add(1)
				}
			}
		}(g)
	}

	// Let some events process then close concurrently
	time.Sleep(10 * time.Millisecond)
	_ = client.Close()

	wg.Wait()

	total := successCount.Load() + dropCount.Load()
	expectedTotal := int64(numGoroutines * eventsPerGoroutine)
	if total != expectedTotal {
		t.Fatalf("expected %d total attempts, got %d", expectedTotal, total)
	}
}
