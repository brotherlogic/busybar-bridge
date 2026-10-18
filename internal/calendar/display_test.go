package calendar

import (
	"bytes"
	"strings"
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
