package calendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
	"github.com/brotherlogic/busybar-bridge/pkg/pb"
)

func TestFormatCountdown(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"45 seconds rounds up to 1 minute", 45 * time.Second, "00:01"},
		{"14 minutes 30 seconds rounds up to 15 minutes", 14*time.Minute + 30*time.Second, "00:15"},
		{"14 minutes exact stays 14 minutes", 14 * time.Minute, "00:14"},
		{"1 second rounds up to 1 minute", 1 * time.Second, "00:01"},
		{"zero duration is 00:00", 0, "00:00"},
		{"negative duration is 00:00", -5 * time.Minute, "00:00"},
		{"26 hours 15 minutes", 26*time.Hour + 15*time.Minute, "26:15"},
		{"26 hours 14 minutes 30 seconds rounds up to 26:15", 26*time.Hour + 14*time.Minute + 30*time.Second, "26:15"},
		{"100 hours exact", 100 * time.Hour, "100:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCountdown(tt.duration)
			if got != tt.expected {
				t.Errorf("FormatCountdown(%v) = %q; want %q", tt.duration, got, tt.expected)
			}
		})
	}
}

func createTestStore(t *testing.T, bound bool) *Store {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "calendar_test.pb")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if bound {
		err = store.Save(&pb.CalendarBinding{
			Email:           "tester@example.com",
			CalendarId:      "primary",
			AccessToken:     "test-access-token",
			RefreshToken:    "test-refresh-token",
			TokenExpiryUnix: time.Now().Add(1 * time.Hour).Unix(),
			LinkedAtUnix:    time.Now().Unix(),
		})
		if err != nil {
			t.Fatalf("failed to save binding: %v", err)
		}
	}

	return store
}

func TestPoller_EventFiltering(t *testing.T) {
	refTime := time.Date(2026, 10, 1, 10, 15, 0, 0, time.UTC)

	// Mock server returning various events
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"items": []map[string]interface{}{
				// 1. All-day event (date only) -> should be excluded
				{
					"id":      "allday1",
					"summary": "All Day Event",
					"start":   map[string]string{"date": "2026-10-01"},
					"end":     map[string]string{"date": "2026-10-02"},
				},
				// 2. Cancelled event -> should be excluded
				{
					"id":      "cancelled1",
					"summary": "Cancelled Meeting",
					"status":  "cancelled",
					"start":   map[string]string{"dateTime": refTime.Add(-15 * time.Minute).Format(time.RFC3339)},
					"end":     map[string]string{"dateTime": refTime.Add(45 * time.Minute).Format(time.RFC3339)},
				},
				// 3. Transparent (free) event -> should be excluded
				{
					"id":           "trans1",
					"summary":      "Transparent Free Block",
					"transparency": "transparent",
					"start":        map[string]string{"dateTime": refTime.Add(-15 * time.Minute).Format(time.RFC3339)},
					"end":          map[string]string{"dateTime": refTime.Add(45 * time.Minute).Format(time.RFC3339)},
				},
				// 4. Declined invitation -> should be excluded
				{
					"id":      "declined1",
					"summary": "Declined Meeting",
					"start":   map[string]string{"dateTime": refTime.Add(-15 * time.Minute).Format(time.RFC3339)},
					"end":     map[string]string{"dateTime": refTime.Add(45 * time.Minute).Format(time.RFC3339)},
					"attendees": []map[string]interface{}{
						{"email": "tester@example.com", "self": true, "responseStatus": "declined"},
					},
				},
				// 5. Eligible active event -> should be selected
				{
					"id":      "eligible1",
					"summary": "Valid Active Meeting",
					"status":  "confirmed",
					"start":   map[string]string{"dateTime": refTime.Add(-15 * time.Minute).Format(time.RFC3339)},
					"end":     map[string]string{"dateTime": refTime.Add(45 * time.Minute).Format(time.RFC3339)},
					"attendees": []map[string]interface{}{
						{"email": "other@example.com", "responseStatus": "declined"},
						{"email": "tester@example.com", "self": true, "responseStatus": "accepted"},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	store := createTestStore(t, true)
	mgr := NewManager(ManagerConfig{})
	telStore := telemetry.NewStore()

	poller, err := NewPoller(PollerConfig{
		Enabled:         true,
		PollInterval:    time.Minute,
		Store:           store,
		Manager:         mgr,
		Telemetry:       telStore,
		CalendarBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Now:             func() time.Time { return refTime },
	})
	if err != nil {
		t.Fatalf("failed to create poller: %v", err)
	}

	ctx := context.Background()
	if err := poller.Poll(ctx); err != nil {
		t.Fatalf("Poll failed: %v", err)
	}

	curr := poller.GetCurrentEvent()
	if curr.Idle {
		t.Fatalf("expected active event, got idle")
	}
	if curr.Summary != "Valid Active Meeting" {
		t.Errorf("expected summary %q, got %q", "Valid Active Meeting", curr.Summary)
	}
	// refTime is 10:15, end is 11:00 (45 minutes remaining)
	if curr.Countdown != "00:45" {
		t.Errorf("expected countdown %q, got %q", "00:45", curr.Countdown)
	}

	// Verify telemetry recorded poll
	snap := telStore.Snapshot()
	if snap.CalendarSync.TotalPolls != 1 || snap.CalendarSync.SuccessPolls != 1 {
		t.Errorf("unexpected telemetry polls: %+v", snap.CalendarSync)
	}
	if snap.CalendarSync.ActiveSummary != "Valid Active Meeting" {
		t.Errorf("expected ActiveSummary %q, got %q", "Valid Active Meeting", snap.CalendarSync.ActiveSummary)
	}
}

func TestPoller_OverlapPriorityAndTieBreaking(t *testing.T) {
	refTime := time.Date(2026, 10, 1, 10, 20, 0, 0, time.UTC)

	// Scenario:
	// Event A: 10:00 -> 11:00
	// Event B: 10:00 -> 10:30 (earlier end time than A, so should beat A)
	// Event C: 10:15 -> 10:30 (same end time as B, but later start time than B: 10:15 > 10:00, should beat B)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":      "eventA",
					"summary": "Meeting A (10:00 - 11:00)",
					"start":   map[string]string{"dateTime": refTime.Add(-20 * time.Minute).Format(time.RFC3339)}, // 10:00
					"end":     map[string]string{"dateTime": refTime.Add(40 * time.Minute).Format(time.RFC3339)},  // 11:00
				},
				{
					"id":      "eventB",
					"summary": "Meeting B (10:00 - 10:30)",
					"start":   map[string]string{"dateTime": refTime.Add(-20 * time.Minute).Format(time.RFC3339)}, // 10:00
					"end":     map[string]string{"dateTime": refTime.Add(10 * time.Minute).Format(time.RFC3339)},  // 10:30
				},
				{
					"id":      "eventC",
					"summary": "Meeting C (10:15 - 10:30)",
					"start":   map[string]string{"dateTime": refTime.Add(-5 * time.Minute).Format(time.RFC3339)},  // 10:15
					"end":     map[string]string{"dateTime": refTime.Add(10 * time.Minute).Format(time.RFC3339)},  // 10:30
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	store := createTestStore(t, true)
	mgr := NewManager(ManagerConfig{})
	telStore := telemetry.NewStore()

	poller, err := NewPoller(PollerConfig{
		Enabled:         true,
		PollInterval:    time.Minute,
		Store:           store,
		Manager:         mgr,
		Telemetry:       telStore,
		CalendarBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Now:             func() time.Time { return refTime },
	})
	if err != nil {
		t.Fatalf("failed to create poller: %v", err)
	}

	if err := poller.Poll(context.Background()); err != nil {
		t.Fatalf("Poll failed: %v", err)
	}

	curr := poller.GetCurrentEvent()
	if curr.Summary != "Meeting C (10:15 - 10:30)" {
		t.Errorf("expected tie-breaker winner %q, got %q", "Meeting C (10:15 - 10:30)", curr.Summary)
	}
	// 10 minutes remaining
	if curr.Countdown != "00:10" {
		t.Errorf("expected countdown %q, got %q", "00:10", curr.Countdown)
	}
}

func TestPoller_TransitionToIdle(t *testing.T) {
	currTime := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	var timeMu sync.Mutex
	getTime := func() time.Time {
		timeMu.Lock()
		defer timeMu.Unlock()
		return currTime
	}
	setTime := func(newT time.Time) {
		timeMu.Lock()
		defer timeMu.Unlock()
		currTime = newT
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":      "event1",
					"summary": "Short Standup",
					"start":   map[string]string{"dateTime": currTime.Format(time.RFC3339)},
					"end":     map[string]string{"dateTime": currTime.Add(15 * time.Minute).Format(time.RFC3339)},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	store := createTestStore(t, true)
	mgr := NewManager(ManagerConfig{})
	telStore := telemetry.NewStore()

	poller, err := NewPoller(PollerConfig{
		Enabled:         true,
		PollInterval:    time.Minute,
		Store:           store,
		Manager:         mgr,
		Telemetry:       telStore,
		CalendarBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Now:             getTime,
	})
	if err != nil {
		t.Fatalf("failed to create poller: %v", err)
	}

	if err := poller.Poll(context.Background()); err != nil {
		t.Fatalf("Poll failed: %v", err)
	}

	// At 10:00: active, 15m remaining
	curr := poller.GetCurrentEvent()
	if curr.Idle || curr.Countdown != "00:15" {
		t.Errorf("expected active countdown 00:15, got Idle=%v, Countdown=%q", curr.Idle, curr.Countdown)
	}

	// Advance time to 10:14:30 -> 30s remaining -> rounds up to 00:01
	setTime(currTime.Add(14*time.Minute + 30*time.Second))
	curr = poller.GetCurrentEvent()
	if curr.Idle || curr.Countdown != "00:01" {
		t.Errorf("expected countdown 00:01, got Idle=%v, Countdown=%q", curr.Idle, curr.Countdown)
	}

	// Advance time to 10:15:00 -> exactly end time -> must immediately transition to idle
	setTime(currTime.Add(15 * time.Minute))
	curr = poller.GetCurrentEvent()
	if !curr.Idle {
		t.Errorf("expected immediate transition to idle, got %+v", curr)
	}
	if curr.Countdown != "" {
		t.Errorf("expected empty countdown on idle, got %q", curr.Countdown)
	}
}

func TestPoller_OfflineFaultTolerance(t *testing.T) {
	currTime := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	var timeMu sync.Mutex
	getTime := func() time.Time {
		timeMu.Lock()
		defer timeMu.Unlock()
		return currTime
	}
	setTime := func(newT time.Time) {
		timeMu.Lock()
		defer timeMu.Unlock()
		currTime = newT
	}

	var shouldFail atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldFail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": "internal google server error"}`))
			return
		}

		resp := map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":      "active1",
					"summary": "Critical Sync Meeting",
					"start":   map[string]string{"dateTime": currTime.Format(time.RFC3339)},
					"end":     map[string]string{"dateTime": currTime.Add(30 * time.Minute).Format(time.RFC3339)},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	store := createTestStore(t, true)
	mgr := NewManager(ManagerConfig{})
	telStore := telemetry.NewStore()

	poller, err := NewPoller(PollerConfig{
		Enabled:         true,
		PollInterval:    time.Minute,
		Store:           store,
		Manager:         mgr,
		Telemetry:       telStore,
		CalendarBaseURL: server.URL,
		HTTPClient:      server.Client(),
		Now:             getTime,
	})
	if err != nil {
		t.Fatalf("failed to create poller: %v", err)
	}

	// 1. Initial successful poll
	if err := poller.Poll(context.Background()); err != nil {
		t.Fatalf("initial poll failed: %v", err)
	}
	curr := poller.GetCurrentEvent()
	if curr.Idle || curr.Summary != "Critical Sync Meeting" || curr.Countdown != "00:30" {
		t.Fatalf("unexpected active event: %+v", curr)
	}

	// 2. Next poll fails due to API error
	shouldFail.Store(true)
	// Advance time to 10:10 (20 minutes remaining)
	setTime(currTime.Add(10 * time.Minute))

	err = poller.Poll(context.Background())
	if err == nil {
		t.Fatalf("expected poll error when server returns 500")
	}

	// Event should STILL be retained and countdown continuing locally!
	curr = poller.GetCurrentEvent()
	if curr.Idle {
		t.Fatalf("expected retained active event during outage, got idle")
	}
	if curr.Summary != "Critical Sync Meeting" {
		t.Errorf("expected summary %q, got %q", "Critical Sync Meeting", curr.Summary)
	}
	if curr.Countdown != "00:20" {
		t.Errorf("expected countdown %q during outage, got %q", "00:20", curr.Countdown)
	}

	// Telemetry should record failed poll but maintain active summary
	snap := telStore.Snapshot()
	if snap.CalendarSync.FailedPolls != 1 {
		t.Errorf("expected 1 failed poll in telemetry, got %d", snap.CalendarSync.FailedPolls)
	}
	if snap.CalendarSync.ActiveSummary != "Critical Sync Meeting" {
		t.Errorf("expected active summary %q in telemetry, got %q", "Critical Sync Meeting", snap.CalendarSync.ActiveSummary)
	}

	// 3. Time passes past end time (10:35) during outage
	setTime(currTime.Add(35 * time.Minute))
	curr = poller.GetCurrentEvent()
	if !curr.Idle {
		t.Errorf("expected transition to idle when end time passes during outage, got %+v", curr)
	}

	// Another poll during outage after end time
	err = poller.Poll(context.Background())
	if err == nil {
		t.Fatalf("expected poll error")
	}
	snap = telStore.Snapshot()
	if snap.CalendarSync.ActiveSummary != "" {
		t.Errorf("expected empty active summary in telemetry after event end, got %q", snap.CalendarSync.ActiveSummary)
	}
}

func TestPoller_DormantState(t *testing.T) {
	t.Run("unbound store remains dormant", func(t *testing.T) {
		requestsReceived := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestsReceived++
		}))
		defer server.Close()

		store := createTestStore(t, false) // unbound
		mgr := NewManager(ManagerConfig{})
		telStore := telemetry.NewStore()

		poller, err := NewPoller(PollerConfig{
			Enabled:         true,
			PollInterval:    time.Minute,
			Store:           store,
			Manager:         mgr,
			Telemetry:       telStore,
			CalendarBaseURL: server.URL,
			HTTPClient:      server.Client(),
		})
		if err != nil {
			t.Fatalf("failed to create poller: %v", err)
		}

		if err := poller.Poll(context.Background()); err != nil {
			t.Fatalf("poll on unbound store should not error, got %v", err)
		}

		if requestsReceived != 0 {
			t.Errorf("expected 0 HTTP requests when unbound, got %d", requestsReceived)
		}

		curr := poller.GetCurrentEvent()
		if !curr.Idle {
			t.Errorf("expected idle event when unbound, got %+v", curr)
		}

		snap := telStore.Snapshot()
		if snap.CalendarSync.TotalPolls != 0 {
			t.Errorf("expected 0 recorded polls when unbound, got %d", snap.CalendarSync.TotalPolls)
		}
	})

	t.Run("sync disabled remains dormant", func(t *testing.T) {
		requestsReceived := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestsReceived++
		}))
		defer server.Close()

		store := createTestStore(t, true) // bound
		mgr := NewManager(ManagerConfig{})
		telStore := telemetry.NewStore()

		poller, err := NewPoller(PollerConfig{
			Enabled:         false, // disabled
			PollInterval:    time.Minute,
			Store:           store,
			Manager:         mgr,
			Telemetry:       telStore,
			CalendarBaseURL: server.URL,
			HTTPClient:      server.Client(),
		})
		if err != nil {
			t.Fatalf("failed to create poller: %v", err)
		}

		if err := poller.Poll(context.Background()); err != nil {
			t.Fatalf("poll when disabled should not error, got %v", err)
		}

		if requestsReceived != 0 {
			t.Errorf("expected 0 HTTP requests when disabled, got %d", requestsReceived)
		}

		curr := poller.GetCurrentEvent()
		if !curr.Idle {
			t.Errorf("expected idle event when disabled, got %+v", curr)
		}
	})
}

func TestPoller_LifecycleAndConcurrency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		resp := map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":      "event1",
					"summary": "Concurrent Meeting",
					"start":   map[string]string{"dateTime": now.Add(-5 * time.Minute).Format(time.RFC3339)},
					"end":     map[string]string{"dateTime": now.Add(25 * time.Minute).Format(time.RFC3339)},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	store := createTestStore(t, true)
	mgr := NewManager(ManagerConfig{})
	telStore := telemetry.NewStore()

	poller, err := NewPoller(PollerConfig{
		Enabled:         true,
		PollInterval:    20 * time.Millisecond,
		Store:           store,
		Manager:         mgr,
		Telemetry:       telStore,
		CalendarBaseURL: server.URL,
		HTTPClient:      server.Client(),
	})
	if err != nil {
		t.Fatalf("failed to create poller: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := poller.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Double start should return error
	if err := poller.Start(ctx); err == nil {
		t.Fatalf("expected error on duplicate Start")
	}

	// Concurrently query GetCurrentEvent while polling is ongoing
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				ev := poller.GetCurrentEvent()
				_ = ev.Countdown
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}
	wg.Wait()

	if err := poller.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Double close should be idempotent
	if err := poller.Close(); err != nil {
		t.Fatalf("expected nil on idempotent Close: %v", err)
	}
}
