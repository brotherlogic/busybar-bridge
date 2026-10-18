package calendar

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/brotherlogic/busybar-bridge/internal/busybar"
	pb_busybar "github.com/brotherlogic/busybar-bridge/pkg/pb/busybar"
)

func countLitPixelsInRows(data []byte, startY, endY int) int {
	lit := 0
	for y := startY; y < endY; y++ {
		for x := 0; x < DisplayWidth; x++ {
			idx := (y*DisplayWidth + x) * DisplayBytesPerPixel
			if idx+2 < len(data) {
				if data[idx] != 0 || data[idx+1] != 0 || data[idx+2] != 0 {
					lit++
				}
			}
		}
	}
	return lit
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		name     string
		summary  string
		maxChars int
		want     string
	}{
		{
			name:     "empty string falls back to Busy",
			summary:  "",
			maxChars: MaxDisplayChars,
			want:     FallbackEventTitle,
		},
		{
			name:     "whitespace-only string falls back to Busy",
			summary:  "   \t\n  ",
			maxChars: MaxDisplayChars,
			want:     FallbackEventTitle,
		},
		{
			name:     "short title with surrounding whitespace is trimmed",
			summary:  "  Team Sync  ",
			maxChars: MaxDisplayChars,
			want:     "Team Sync",
		},
		{
			name:     "exact 14 char ASCII title is unchanged",
			summary:  "12345678901234",
			maxChars: MaxDisplayChars,
			want:     "12345678901234",
		},
		{
			name:     "long ASCII title is truncated to 14 chars",
			summary:  "Architecture Review Meeting With Engineering",
			maxChars: MaxDisplayChars,
			want:     "Architecture R",
		},
		{
			name:     "multi-byte UTF-8 accented runes truncate on rune boundaries",
			summary:  "Café Résumé Naïve Über Straße Meeting",
			maxChars: MaxDisplayChars,
			want:     "Café Résumé Na",
		},
		{
			name:     "multi-byte UTF-8 emojis truncate cleanly at 14 runes",
			summary:  "📅🚀🔥🎉✨💡🛠️📦🔍🔒🌐⚡🎯🏆 Extra",
			maxChars: MaxDisplayChars,
			want:     "📅🚀🔥🎉✨💡🛠️📦🔍🔒🌐⚡🎯",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TruncateTitle(tc.summary, tc.maxChars)
			if !utf8.ValidString(got) {
				t.Fatalf("TruncateTitle(%q, %d) produced invalid UTF-8: %q", tc.summary, tc.maxChars, got)
			}
			if got != tc.want {
				t.Errorf("TruncateTitle(%q, %d) = %q (runes=%d), want %q (runes=%d)",
					tc.summary, tc.maxChars, got, utf8.RuneCountInString(got), tc.want, utf8.RuneCountInString(tc.want))
			}
			if utf8.RuneCountInString(got) > tc.maxChars {
				t.Errorf("TruncateTitle(%q, %d) rune count = %d, exceeds maxChars %d",
					tc.summary, tc.maxChars, utf8.RuneCountInString(got), tc.maxChars)
			}
		})
	}
}

func TestFormatDisplayState_ActiveAndIdle(t *testing.T) {
	loc := time.FixedZone("PDT", -7*3600)
	now := time.Date(2026, time.October, 18, 14, 35, 0, 0, loc)

	t.Run("active event with populated countdown", func(t *testing.T) {
		ev := ActiveEvent{
			Summary:   "Quarterly Planning Session",
			StartTime: now.Add(-15 * time.Minute),
			EndTime:   now.Add(45 * time.Minute),
			Countdown: "00:45",
			Idle:      false,
		}

		state := FormatDisplayState(ev, now)
		if state.Mode != DisplayModeBusy {
			t.Errorf("Mode = %q, want %q", state.Mode, DisplayModeBusy)
		}
		if state.TopLine != "Quarterly Plan" {
			t.Errorf("TopLine = %q, want %q", state.TopLine, "Quarterly Plan")
		}
		if state.BottomLine != "00:45" {
			t.Errorf("BottomLine = %q, want %q", state.BottomLine, "00:45")
		}
		if !state.UpdatedAt.Equal(now) {
			t.Errorf("UpdatedAt = %v, want %v", state.UpdatedAt, now)
		}
	})

	t.Run("active event computes countdown when empty and falls back to Busy on blank summary", func(t *testing.T) {
		ev := ActiveEvent{
			Summary:   "   \t\n ",
			StartTime: now.Add(-10 * time.Minute),
			EndTime:   now.Add(90 * time.Minute),
			Countdown: "",
			Idle:      false,
		}

		state := FormatDisplayState(ev, now)
		if state.Mode != DisplayModeBusy {
			t.Errorf("Mode = %q, want %q", state.Mode, DisplayModeBusy)
		}
		if state.TopLine != FallbackEventTitle {
			t.Errorf("TopLine = %q, want %q", state.TopLine, FallbackEventTitle)
		}
		if state.BottomLine != "01:30" {
			t.Errorf("BottomLine = %q, want %q", state.BottomLine, "01:30")
		}
	})

	t.Run("idle event formats local date and 24-hour time", func(t *testing.T) {
		ev := ActiveEvent{Idle: true}
		state := FormatDisplayState(ev, now)

		wantDate := now.Local().Format("Mon Jan 02")
		wantTime := now.Local().Format("15:04")

		if state.Mode != DisplayModeIdle {
			t.Errorf("Mode = %q, want %q", state.Mode, DisplayModeIdle)
		}
		if state.TopLine != wantDate {
			t.Errorf("TopLine = %q, want %q", state.TopLine, wantDate)
		}
		if state.BottomLine != wantTime {
			t.Errorf("BottomLine = %q, want %q", state.BottomLine, wantTime)
		}
		if !state.UpdatedAt.Equal(now) {
			t.Errorf("UpdatedAt = %v, want %v", state.UpdatedAt, now)
		}
	})
}

func TestRenderFrame_GeometryAndModes(t *testing.T) {
	now := time.Date(2026, time.October, 18, 9, 42, 0, 0, time.UTC)

	busyState := FormatDisplayState(ActiveEvent{
		Summary:   "Design Sync",
		EndTime:   now.Add(25 * time.Minute),
		Countdown: "00:25",
		Idle:      false,
	}, now)

	busyFrame := RenderFrame(busyState)
	if busyFrame == nil {
		t.Fatal("RenderFrame(busyState) returned nil")
	}
	if busyFrame.GetScreen() != busybar.ScreenFront {
		t.Errorf("Screen = %v, want %v", busyFrame.GetScreen(), busybar.ScreenFront)
	}
	if busyFrame.GetWidth() != DisplayWidth {
		t.Errorf("Width = %d, want %d", busyFrame.GetWidth(), DisplayWidth)
	}
	if busyFrame.GetHeight() != DisplayHeight {
		t.Errorf("Height = %d, want %d", busyFrame.GetHeight(), DisplayHeight)
	}
	if busyFrame.GetEncoding() != pb_busybar.Encoding_PLAIN {
		t.Errorf("Encoding = %v, want %v", busyFrame.GetEncoding(), pb_busybar.Encoding_PLAIN)
	}
	if busyFrame.GetPixelFormat() != pb_busybar.PixelFormat_RGB888 {
		t.Errorf("PixelFormat = %v, want %v", busyFrame.GetPixelFormat(), pb_busybar.PixelFormat_RGB888)
	}
	if len(busyFrame.GetData()) != DisplayBufferSize {
		t.Fatalf("len(Data) = %d, want %d", len(busyFrame.GetData()), DisplayBufferSize)
	}

	topLit := countLitPixelsInRows(busyFrame.GetData(), 0, 8)
	bottomLit := countLitPixelsInRows(busyFrame.GetData(), 8, 16)
	if topLit == 0 {
		t.Error("expected non-zero lit pixels in top band (y in [0,8)) for active event")
	}
	if bottomLit == 0 {
		t.Error("expected non-zero lit pixels in bottom band (y in [8,16)) for active event")
	}

	idleState := FormatDisplayState(ActiveEvent{Idle: true}, now)
	idleFrame := RenderFrame(idleState)
	if idleFrame == nil {
		t.Fatal("RenderFrame(idleState) returned nil")
	}
	if len(idleFrame.GetData()) != DisplayBufferSize {
		t.Fatalf("len(idleFrame.Data) = %d, want %d", len(idleFrame.GetData()), DisplayBufferSize)
	}
	if countLitPixelsInRows(idleFrame.GetData(), 0, 8) == 0 {
		t.Error("expected non-zero lit pixels in top band for idle date")
	}
	if countLitPixelsInRows(idleFrame.GetData(), 8, 16) == 0 {
		t.Error("expected non-zero lit pixels in bottom band for idle time")
	}
	if bytes.Equal(busyFrame.GetData(), idleFrame.GetData()) {
		t.Error("expected busy frame and idle frame pixel buffers to differ")
	}
}

func TestRenderFrame_MultiDayCountdownsAndLongUTF8(t *testing.T) {
	now := time.Date(2026, time.October, 18, 10, 0, 0, 0, time.UTC)

	for _, countdown := range []string{"26:15", "120:45"} {
		state := FormatDisplayState(ActiveEvent{
			Summary:   "🌐 Global Offsite & Hackathon 🚀🎉",
			Countdown: countdown,
			Idle:      false,
		}, now)

		if !utf8.ValidString(state.TopLine) {
			t.Fatalf("invalid UTF-8 TopLine: %q", state.TopLine)
		}
		if state.BottomLine != countdown {
			t.Fatalf("BottomLine = %q, want %q", state.BottomLine, countdown)
		}

		frame := RenderFrame(state)
		if len(frame.GetData()) != DisplayBufferSize {
			t.Fatalf("countdown %q: len(Data) = %d, want %d", countdown, len(frame.GetData()), DisplayBufferSize)
		}
		if countLitPixelsInRows(frame.GetData(), 0, 8) == 0 {
			t.Errorf("countdown %q: expected lit pixels in top band", countdown)
		}
		if countLitPixelsInRows(frame.GetData(), 8, 16) == 0 {
			t.Errorf("countdown %q: expected lit pixels in bottom band", countdown)
		}
	}

	// Verify extreme overflowing lines passed directly to RenderFrame clamp safely without panicking
	oversizedFrame := RenderFrame(DisplayState{
		Mode:       DisplayModeBusy,
		TopLine:    strings.Repeat("W", 100),
		BottomLine: strings.Repeat("8", 100),
	})
	if len(oversizedFrame.GetData()) != DisplayBufferSize {
		t.Fatalf("oversized frame len(Data) = %d, want %d", len(oversizedFrame.GetData()), DisplayBufferSize)
	}
}

type mockEventTracker struct {
	mu sync.RWMutex
	ev ActiveEvent
}

func (m *mockEventTracker) GetCurrentEvent() ActiveEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ev
}

func (m *mockEventTracker) SetEvent(ev ActiveEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ev = ev
}

type mockPushClient struct {
	mu      sync.Mutex
	frames  []*busybar.Frame
	pushErr error
	pushCh  chan *busybar.Frame
}

func newMockPushClient() *mockPushClient {
	return &mockPushClient{
		pushCh: make(chan *busybar.Frame, 32),
	}
}

func (m *mockPushClient) PushFrame(_ context.Context, frame *busybar.Frame) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pushErr != nil {
		return m.pushErr
	}
	m.frames = append(m.frames, frame)
	select {
	case m.pushCh <- frame:
	default:
	}
	return nil
}

func (m *mockPushClient) Start(_ context.Context) error { return nil }
func (m *mockPushClient) Close() error                  { return nil }
func (m *mockPushClient) Config() busybar.PushConfig    { return busybar.DefaultPushConfig() }

func (m *mockPushClient) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.frames)
}

func (m *mockPushClient) LastFrame() *busybar.Frame {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.frames) == 0 {
		return nil
	}
	return m.frames[len(m.frames)-1]
}

func (m *mockPushClient) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pushErr = err
}

func TestDisplayOrchestrator_ImmediateChangeDetectionAndBackToBack(t *testing.T) {
	ctx := context.Background()
	var clockMu sync.Mutex
	now := time.Date(2026, time.October, 18, 10, 0, 0, 0, time.UTC)
	nowFn := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}
	advanceClock := func(d time.Duration) {
		clockMu.Lock()
		now = now.Add(d)
		clockMu.Unlock()
	}

	tracker := &mockEventTracker{
		ev: ActiveEvent{
			Summary:   "Sprint Review",
			StartTime: now.Add(-30 * time.Minute),
			EndTime:   now.Add(30 * time.Minute),
			Countdown: "00:30",
			Idle:      false,
		},
	}
	pushClient := newMockPushClient()

	orch := NewDisplayOrchestrator(OrchestratorConfig{
		Enabled:           true,
		Tracker:           tracker,
		PushClient:        pushClient,
		EvalInterval:      1 * time.Second,
		KeepAliveInterval: 30 * time.Second,
		Now:               nowFn,
	})

	// Before first Step, GetDisplayState should compute on-the-fly from Tracker
	initialState := orch.GetDisplayState()
	if initialState.Mode != DisplayModeBusy || initialState.TopLine != "Sprint Review" || initialState.BottomLine != "00:30" {
		t.Fatalf("unexpected pre-step DisplayState: %+v", initialState)
	}

	// 1. Immediate push on first Step
	if err := orch.Step(ctx); err != nil {
		t.Fatalf("first Step error: %v", err)
	}
	if got := pushClient.Count(); got != 1 {
		t.Fatalf("push count after first Step = %d, want 1", got)
	}

	// 2. Duplicate push suppressed when state is unchanged within < 30s
	advanceClock(10 * time.Second)
	if err := orch.Step(ctx); err != nil {
		t.Fatalf("second Step error: %v", err)
	}
	if got := pushClient.Count(); got != 1 {
		t.Fatalf("push count after unchanged Step within <30s = %d, want 1", got)
	}

	// 3. Immediate push on minute rollover
	advanceClock(50 * time.Second)
	tracker.SetEvent(ActiveEvent{
		Summary:   "Sprint Review",
		StartTime: nowFn().Add(-31 * time.Minute),
		EndTime:   nowFn().Add(29 * time.Minute),
		Countdown: "00:29",
		Idle:      false,
	})
	if err := orch.Step(ctx); err != nil {
		t.Fatalf("minute rollover Step error: %v", err)
	}
	if got := pushClient.Count(); got != 2 {
		t.Fatalf("push count after minute rollover = %d, want 2", got)
	}
	if st := orch.GetDisplayState(); st.BottomLine != "00:29" {
		t.Errorf("GetDisplayState().BottomLine = %q, want %q", st.BottomLine, "00:29")
	}

	// 4. Back-to-back meeting transition without flashing idle
	advanceClock(5 * time.Second)
	tracker.SetEvent(ActiveEvent{
		Summary:   "Design Sync",
		StartTime: nowFn(),
		EndTime:   nowFn().Add(60 * time.Minute),
		Countdown: "01:00",
		Idle:      false,
	})
	if err := orch.Step(ctx); err != nil {
		t.Fatalf("back-to-back transition Step error: %v", err)
	}
	if got := pushClient.Count(); got != 3 {
		t.Fatalf("push count after back-to-back transition = %d, want 3", got)
	}
	if st := orch.GetDisplayState(); st.Mode != DisplayModeBusy || st.TopLine != "Design Sync" || st.BottomLine != "01:00" {
		t.Errorf("unexpected state after back-to-back meeting: %+v", st)
	}

	// 5. Immediate transition to idle when meeting ends
	advanceClock(5 * time.Second)
	tracker.SetEvent(ActiveEvent{Idle: true})
	if err := orch.Step(ctx); err != nil {
		t.Fatalf("idle transition Step error: %v", err)
	}
	if got := pushClient.Count(); got != 4 {
		t.Fatalf("push count after idle transition = %d, want 4", got)
	}
	if st := orch.GetDisplayState(); st.Mode != DisplayModeIdle {
		t.Errorf("expected idle mode after meeting ended, got %+v", st)
	}
}

func TestDisplayOrchestrator_KeepAliveRefresh(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)

	tracker := &mockEventTracker{
		ev: ActiveEvent{
			Summary:   "Deep Work",
			Countdown: "01:00",
			Idle:      false,
		},
	}
	pushClient := newMockPushClient()

	orch := NewDisplayOrchestrator(OrchestratorConfig{
		Enabled:           true,
		Tracker:           tracker,
		PushClient:        pushClient,
		EvalInterval:      1 * time.Second,
		KeepAliveInterval: 30 * time.Second,
		Now:               func() time.Time { return now },
	})

	// Initial push at t=0
	if err := orch.Step(ctx); err != nil {
		t.Fatalf("initial Step error: %v", err)
	}
	if got := pushClient.Count(); got != 1 {
		t.Fatalf("push count at t=0 = %d, want 1", got)
	}

	// At t=29s, state unchanged -> no push
	now = now.Add(29 * time.Second)
	if err := orch.Step(ctx); err != nil {
		t.Fatalf("Step at t=29s error: %v", err)
	}
	if got := pushClient.Count(); got != 1 {
		t.Fatalf("push count at t=29s = %d, want 1", got)
	}

	// At t=30s, state unchanged -> keep-alive push triggered
	now = now.Add(1 * time.Second)
	if err := orch.Step(ctx); err != nil {
		t.Fatalf("Step at t=30s error: %v", err)
	}
	if got := pushClient.Count(); got != 2 {
		t.Fatalf("push count at t=30s = %d, want 2", got)
	}
}

func TestDisplayOrchestrator_PushErrorResilienceAndLifecycle(t *testing.T) {
	ctx := context.Background()

	// Disabled orchestrator is a no-op
	disabledOrch := NewDisplayOrchestrator(OrchestratorConfig{Enabled: false})
	if err := disabledOrch.Step(ctx); err != nil {
		t.Fatalf("disabled Step error: %v", err)
	}
	if st := disabledOrch.GetDisplayState(); st.Mode != DisplayModeIdle {
		t.Errorf("disabled GetDisplayState().Mode = %q, want %q", st.Mode, DisplayModeIdle)
	}

	var clockMu sync.Mutex
	now := time.Date(2026, time.October, 18, 15, 0, 0, 0, time.UTC)
	tracker := &mockEventTracker{
		ev: ActiveEvent{
			Summary:   "Incident Response",
			Countdown: "00:45",
			Idle:      false,
		},
	}
	pushClient := newMockPushClient()
	pushClient.SetError(errors.New("device unreachable"))

	orch := NewDisplayOrchestrator(OrchestratorConfig{
		Enabled:           true,
		Tracker:           tracker,
		PushClient:        pushClient,
		EvalInterval:      10 * time.Millisecond,
		KeepAliveInterval: 30 * time.Second,
		Now: func() time.Time {
			clockMu.Lock()
			defer clockMu.Unlock()
			return now
		},
	})

	// Step returns error when PushFrame fails, and does not mark hasPushed so next tick retries
	if err := orch.Step(ctx); err == nil {
		t.Fatal("expected Step to return PushFrame error")
	}
	if got := pushClient.Count(); got != 0 {
		t.Fatalf("push count on error = %d, want 0", got)
	}

	// Clear error and Start background worker
	pushClient.SetError(nil)
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := orch.Start(runCtx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	// Double-start must return an error
	if err := orch.Start(runCtx); err == nil {
		t.Fatal("expected error on second Start call")
	}

	select {
	case <-pushClient.pushCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial startup push from Start()")
	}

	// Close idempotently and verify clean goroutine termination
	if err := orch.Close(); err != nil {
		t.Fatalf("first Close error: %v", err)
	}
	if err := orch.Close(); err != nil {
		t.Fatalf("second Close error: %v", err)
	}

	// Start after Close must return an error
	if err := orch.Start(context.Background()); err == nil {
		t.Fatal("expected error when starting closed orchestrator")
	}
}

