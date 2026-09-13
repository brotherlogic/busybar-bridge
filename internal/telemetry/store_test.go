package telemetry_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
)

func TestInitialBootState(t *testing.T) {
	store := telemetry.NewStore()

	if store.IsConnected() {
		t.Fatalf("expected IsConnected() to be false initially, got true")
	}

	snap := store.Snapshot()

	if snap.StartTime.IsZero() {
		t.Errorf("expected non-zero StartTime")
	}
	if snap.Uptime < 0 {
		t.Errorf("expected non-negative Uptime, got %v", snap.Uptime)
	}
	if snap.Connection.Connected {
		t.Errorf("expected Connection.Connected to be false initially")
	}
	if snap.Connection.ReconnectCount != 0 {
		t.Errorf("expected Connection.ReconnectCount to be 0, got %d", snap.Connection.ReconnectCount)
	}
	if !snap.Connection.LastConnectedAt.IsZero() {
		t.Errorf("expected zero LastConnectedAt initially")
	}
	if !snap.Connection.LastDisconnectedAt.IsZero() {
		t.Errorf("expected zero LastDisconnectedAt initially")
	}

	if snap.Counters.Buttons != 0 || snap.Counters.Switches != 0 || snap.Counters.Encoders != 0 || snap.Counters.Other != 0 {
		t.Errorf("expected all counters to be zero, got %+v", snap.Counters)
	}

	if snap.Forwarding.TotalDispatched != 0 || snap.Forwarding.TotalAcked != 0 || snap.Forwarding.TotalFailed != 0 {
		t.Errorf("expected all forwarding telemetry to be zero, got %+v", snap.Forwarding)
	}

	if snap.TotalEvents != 0 {
		t.Errorf("expected TotalEvents to be 0, got %d", snap.TotalEvents)
	}

	if len(snap.RecentEvents) != 0 {
		t.Errorf("expected 0 RecentEvents initially, got %d", len(snap.RecentEvents))
	}
}

func TestRingBufferCapacityAndEviction(t *testing.T) {
	store := telemetry.NewStore()

	// Inject 25 sequential events
	for i := 1; i <= 25; i++ {
		id := store.RecordEvent("button", fmt.Sprintf("event-%d", i))
		if id != int64(i) {
			t.Fatalf("expected event ID %d, got %d", i, id)
		}
	}

	snap := store.Snapshot()

	if snap.TotalEvents != 25 {
		t.Errorf("expected TotalEvents to be 25, got %d", snap.TotalEvents)
	}

	if snap.Counters.Buttons != 25 {
		t.Errorf("expected Counters.Buttons to be 25, got %d", snap.Counters.Buttons)
	}

	if len(snap.RecentEvents) != 10 {
		t.Fatalf("expected ring buffer capacity of 10 traces, got %d", len(snap.RecentEvents))
	}

	// Verify FIFO order: events 16 through 25
	for idx, trace := range snap.RecentEvents {
		expectedID := int64(16 + idx)
		expectedSummary := fmt.Sprintf("event-%d", 16+idx)
		if trace.ID != expectedID {
			t.Errorf("at index %d: expected ID %d, got %d", idx, expectedID, trace.ID)
		}
		if trace.Summary != expectedSummary {
			t.Errorf("at index %d: expected Summary %s, got %s", idx, expectedSummary, trace.Summary)
		}
		if trace.Outcome != telemetry.OutcomePending {
			t.Errorf("at index %d: expected OutcomePending, got %v", idx, trace.Outcome)
		}
		if trace.Timestamp.IsZero() {
			t.Errorf("at index %d: expected non-zero timestamp", idx)
		}
	}
}

func TestForwardOutcomeRecording(t *testing.T) {
	store := telemetry.NewStore()

	id1 := store.RecordEvent("button", "press ok")
	id2 := store.RecordEvent("switch", "toggle power")

	store.RecordForwardOutcome(id1, telemetry.OutcomeSuccess, 45*time.Millisecond, nil)
	store.RecordForwardOutcome(id2, telemetry.OutcomeFailure, 120*time.Millisecond, errors.New("post failed: 500 Internal Server Error"))

	snap := store.Snapshot()

	if snap.Forwarding.TotalDispatched != 2 {
		t.Errorf("expected TotalDispatched=2, got %d", snap.Forwarding.TotalDispatched)
	}
	if snap.Forwarding.TotalAcked != 1 {
		t.Errorf("expected TotalAcked=1, got %d", snap.Forwarding.TotalAcked)
	}
	if snap.Forwarding.TotalFailed != 1 {
		t.Errorf("expected TotalFailed=1, got %d", snap.Forwarding.TotalFailed)
	}

	if len(snap.RecentEvents) != 2 {
		t.Fatalf("expected 2 recent events, got %d", len(snap.RecentEvents))
	}

	trace1 := snap.RecentEvents[0]
	if trace1.ID != id1 {
		t.Errorf("expected trace1 ID %d, got %d", id1, trace1.ID)
	}
	if trace1.Outcome != telemetry.OutcomeSuccess {
		t.Errorf("expected trace1 OutcomeSuccess, got %s", trace1.Outcome)
	}
	if trace1.LatencyMs != 45 {
		t.Errorf("expected trace1 LatencyMs 45, got %d", trace1.LatencyMs)
	}
	if trace1.Error != "" {
		t.Errorf("expected empty error for trace1, got %s", trace1.Error)
	}

	trace2 := snap.RecentEvents[1]
	if trace2.ID != id2 {
		t.Errorf("expected trace2 ID %d, got %d", id2, trace2.ID)
	}
	if trace2.Outcome != telemetry.OutcomeFailure {
		t.Errorf("expected trace2 OutcomeFailure, got %s", trace2.Outcome)
	}
	if trace2.LatencyMs != 120 {
		t.Errorf("expected trace2 LatencyMs 120, got %d", trace2.LatencyMs)
	}
	if trace2.Error != "post failed: 500 Internal Server Error" {
		t.Errorf("expected trace2 error string, got %s", trace2.Error)
	}
}

func TestConcurrentAccessWithRaceDetector(t *testing.T) {
	store := telemetry.NewStore()

	const numGoroutines = 50
	const iterationsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3)

	// Writer goroutines: RecordEvent
	for g := 0; g < numGoroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < iterationsPerGoroutine; i++ {
				evType := "button"
				switch (gID + i) % 4 {
				case 0:
					evType = "button"
				case 1:
					evType = "switch"
				case 2:
					evType = "encoder"
				case 3:
					evType = "other"
				}
				id := store.RecordEvent(evType, fmt.Sprintf("goroutine-%d-iter-%d", gID, i))
				if i%2 == 0 {
					store.RecordForwardOutcome(id, telemetry.OutcomeSuccess, 10*time.Millisecond, nil)
				} else {
					store.RecordForwardOutcome(id, telemetry.OutcomeFailure, 25*time.Millisecond, errors.New("err"))
				}
			}
		}(g)
	}

	// State modifier goroutines: SetConnected, IncrementReconnectCount
	for g := 0; g < numGoroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < iterationsPerGoroutine; i++ {
				store.SetConnected(i%2 == 0)
				store.IncrementReconnectCount()
			}
		}(g)
	}

	// Reader goroutines: Snapshot and IsConnected
	for g := 0; g < numGoroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterationsPerGoroutine; i++ {
				_ = store.IsConnected()
				snap := store.Snapshot()
				if len(snap.RecentEvents) > 10 {
					t.Errorf("snapshot RecentEvents exceeded capacity of 10: %d", len(snap.RecentEvents))
				}
			}
		}()
	}

	wg.Wait()

	finalSnap := store.Snapshot()
	expectedTotalEvents := int64(numGoroutines * iterationsPerGoroutine)
	if finalSnap.TotalEvents != expectedTotalEvents {
		t.Errorf("expected %d total events, got %d", expectedTotalEvents, finalSnap.TotalEvents)
	}
	if len(finalSnap.RecentEvents) != 10 {
		t.Errorf("expected 10 recent events in ring buffer, got %d", len(finalSnap.RecentEvents))
	}
}

func TestConnectionStatusTransitions(t *testing.T) {
	store := telemetry.NewStore()

	store.SetConnected(true)
	if !store.IsConnected() {
		t.Errorf("expected IsConnected() == true")
	}
	snap := store.Snapshot()
	if !snap.Connection.Connected {
		t.Errorf("expected snapshot Connection.Connected == true")
	}
	if snap.Connection.LastConnectedAt.IsZero() {
		t.Errorf("expected LastConnectedAt to be set")
	}

	store.IncrementReconnectCount()
	store.SetConnected(false)
	if store.IsConnected() {
		t.Errorf("expected IsConnected() == false")
	}
	snap = store.Snapshot()
	if snap.Connection.Connected {
		t.Errorf("expected snapshot Connection.Connected == false")
	}
	if snap.Connection.LastDisconnectedAt.IsZero() {
		t.Errorf("expected LastDisconnectedAt to be set")
	}
	if snap.Connection.ReconnectCount != 1 {
		t.Errorf("expected ReconnectCount == 1, got %d", snap.Connection.ReconnectCount)
	}
}

func TestEventCountersCategories(t *testing.T) {
	store := telemetry.NewStore()

	store.RecordEvent("button", "b1")
	store.RecordEvent("BUTTON", "b2")
	store.RecordEvent("buttons", "b3")
	store.RecordEvent("switch", "s1")
	store.RecordEvent("switches", "s2")
	store.RecordEvent("encoder", "e1")
	store.RecordEvent("encoders", "e2")
	store.RecordEvent("custom", "c1")
	store.RecordEvent("other", "o1")

	snap := store.Snapshot()
	if snap.Counters.Buttons != 3 {
		t.Errorf("expected 3 Buttons, got %d", snap.Counters.Buttons)
	}
	if snap.Counters.Switches != 2 {
		t.Errorf("expected 2 Switches, got %d", snap.Counters.Switches)
	}
	if snap.Counters.Encoders != 2 {
		t.Errorf("expected 2 Encoders, got %d", snap.Counters.Encoders)
	}
	if snap.Counters.Other != 2 {
		t.Errorf("expected 2 Other, got %d", snap.Counters.Other)
	}
	if snap.TotalEvents != 9 {
		t.Errorf("expected 9 TotalEvents, got %d", snap.TotalEvents)
	}
}

func TestRecordForwardOutcomeEvictedTrace(t *testing.T) {
	store := telemetry.NewStore()

	id1 := store.RecordEvent("button", "event 1")

	// Inject 10 more events so id1 is evicted from ring buffer
	for i := 2; i <= 12; i++ {
		store.RecordEvent("button", fmt.Sprintf("event %d", i))
	}

	// Update outcome of evicted event
	store.RecordForwardOutcome(id1, telemetry.OutcomeSuccess, 50*time.Millisecond, nil)

	snap := store.Snapshot()
	if snap.Forwarding.TotalDispatched != 1 {
		t.Errorf("expected TotalDispatched=1, got %d", snap.Forwarding.TotalDispatched)
	}
	if snap.Forwarding.TotalAcked != 1 {
		t.Errorf("expected TotalAcked=1, got %d", snap.Forwarding.TotalAcked)
	}
}
