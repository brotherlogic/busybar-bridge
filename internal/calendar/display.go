package calendar

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/busybar"
	pb_busybar "github.com/brotherlogic/busybar-bridge/pkg/pb/busybar"
)

const (
	// DisplayWidth is the width of the Busy Bar front screen in pixels.
	DisplayWidth = 72
	// DisplayHeight is the height of the Busy Bar front screen in pixels.
	DisplayHeight = 16
	// DisplayBytesPerPixel is the number of bytes per RGB888 pixel.
	DisplayBytesPerPixel = 3
	// DisplayBufferSize is the total byte size of a 72x16 RGB888 frame payload (3456 bytes).
	DisplayBufferSize = DisplayWidth * DisplayHeight * DisplayBytesPerPixel
	// MaxDisplayChars is the maximum number of characters rendered per line (14 * 5px pitch = 70px <= 72px).
	MaxDisplayChars = 14
	// FallbackEventTitle is displayed when an active calendar event has an empty or whitespace-only summary.
	FallbackEventTitle = "Busy"

	// DefaultEvalInterval is the default frequency at which DisplayOrchestrator evaluates state changes.
	DefaultEvalInterval = 1 * time.Second
	// DefaultKeepAliveInterval is the default interval for periodic keep-alive frame pushes when state is unchanged.
	DefaultKeepAliveInterval = 30 * time.Second
)

// DisplayMode represents whether the Busy Bar display is showing an active meeting or idle clock screen.
type DisplayMode string

const (
	// DisplayModeIdle indicates no calendar event is active and the date/time screen is displayed.
	DisplayModeIdle DisplayMode = "idle"
	// DisplayModeBusy indicates an active meeting title and countdown are displayed.
	DisplayModeBusy DisplayMode = "busy"
)

// DisplayState holds the formatted two-line text and mode rendered to the Busy Bar display.
type DisplayState struct {
	Mode       DisplayMode `json:"mode"`
	TopLine    string      `json:"top_line"`
	BottomLine string      `json:"bottom_line"`
	UpdatedAt  time.Time   `json:"updated_at,omitempty"`
}

// DisplayProvider exposes thread-safe read access to the latest formatted DisplayState.
type DisplayProvider interface {
	GetDisplayState() DisplayState
}

// OrchestratorConfig defines the configuration parameters for DisplayOrchestrator.
type OrchestratorConfig struct {
	Enabled           bool
	Tracker           EventTracker
	PushClient        busybar.PushClient
	EvalInterval      time.Duration
	KeepAliveInterval time.Duration
	Now               func() time.Time
}

// DisplayOrchestrator monitors EventTracker and the system clock, pushing newly rendered
// frames immediately on state changes and enforcing a periodic keep-alive refresh.
type DisplayOrchestrator struct {
	cfg        OrchestratorConfig
	now        func() time.Time
	mu         sync.RWMutex
	lastState  DisplayState
	lastPushAt time.Time
	hasPushed  bool

	started   atomic.Bool
	closed    atomic.Bool
	closeOnce sync.Once
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewDisplayOrchestrator initializes a DisplayOrchestrator with defaults applied for unset intervals and clock.
func NewDisplayOrchestrator(cfg OrchestratorConfig) *DisplayOrchestrator {
	if cfg.EvalInterval <= 0 {
		cfg.EvalInterval = DefaultEvalInterval
	}
	if cfg.KeepAliveInterval <= 0 {
		cfg.KeepAliveInterval = DefaultKeepAliveInterval
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	cfg.Now = nowFn

	return &DisplayOrchestrator{
		cfg: cfg,
		now: nowFn,
	}
}

// Step evaluates the current calendar and clock state and dispatches a rendered Frame to PushClient
// when the displayed content changes or the keep-alive interval has elapsed.
func (o *DisplayOrchestrator) Step(ctx context.Context) error {
	if o == nil || !o.cfg.Enabled || o.cfg.Tracker == nil || o.cfg.PushClient == nil {
		return nil
	}

	now := o.now()
	ev := o.cfg.Tracker.GetCurrentEvent()
	state := FormatDisplayState(ev, now)

	o.mu.RLock()
	needPush := !o.hasPushed ||
		state.Mode != o.lastState.Mode ||
		state.TopLine != o.lastState.TopLine ||
		state.BottomLine != o.lastState.BottomLine ||
		now.Sub(o.lastPushAt) >= o.cfg.KeepAliveInterval
	o.mu.RUnlock()

	if !needPush {
		return nil
	}

	frame := RenderFrame(state)
	if err := o.cfg.PushClient.PushFrame(ctx, frame); err != nil {
		return err
	}

	o.mu.Lock()
	o.lastState = state
	o.lastPushAt = now
	o.hasPushed = true
	o.mu.Unlock()

	return nil
}

// GetDisplayState returns the latest formatted DisplayState in a thread-safe manner,
// computing on-the-fly from Tracker if no frame has been pushed yet.
func (o *DisplayOrchestrator) GetDisplayState() DisplayState {
	if o == nil {
		return FormatDisplayState(ActiveEvent{Idle: true}, time.Now())
	}

	o.mu.RLock()
	hasPushed := o.hasPushed
	lastState := o.lastState
	o.mu.RUnlock()

	if hasPushed {
		return lastState
	}

	now := o.now()
	ev := ActiveEvent{Idle: true}
	if o.cfg.Tracker != nil {
		ev = o.cfg.Tracker.GetCurrentEvent()
	}
	return FormatDisplayState(ev, now)
}

// Start launches the background display evaluation and keep-alive worker loop.
func (o *DisplayOrchestrator) Start(ctx context.Context) error {
	if o.closed.Load() {
		return errors.New("display orchestrator is closed")
	}
	if o.started.Swap(true) {
		return errors.New("display orchestrator already started")
	}

	workerCtx, cancel := context.WithCancel(ctx)
	o.mu.Lock()
	o.cancel = cancel
	o.mu.Unlock()

	o.wg.Add(1)
	go o.loop(workerCtx)

	return nil
}

// Close terminates the background worker loop and waits for completion.
func (o *DisplayOrchestrator) Close() error {
	o.closeOnce.Do(func() {
		o.closed.Store(true)
		o.mu.Lock()
		cancel := o.cancel
		o.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		o.wg.Wait()
	})
	return nil
}

func (o *DisplayOrchestrator) loop(ctx context.Context) {
	defer o.wg.Done()

	// Immediate evaluation and push on startup
	_ = o.Step(ctx)

	interval := o.cfg.EvalInterval
	if interval <= 0 {
		interval = DefaultEvalInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = o.Step(ctx)
		}
	}
}

// TruncateTitle trims surrounding whitespace from summary, falls back to FallbackEventTitle ("Busy")
// if empty, and truncates cleanly at maxChars on UTF-8 rune boundaries.
func TruncateTitle(summary string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		trimmed = FallbackEventTitle
	}
	runes := []rune(trimmed)
	if len(runes) > maxChars {
		return string(runes[:maxChars])
	}
	return trimmed
}

// FormatDisplayState converts an ActiveEvent and timestamp into a deterministic two-line DisplayState.
func FormatDisplayState(ev ActiveEvent, now time.Time) DisplayState {
	if ev.Idle {
		localNow := now.Local()
		return DisplayState{
			Mode:       DisplayModeIdle,
			TopLine:    localNow.Format("Mon Jan 02"),
			BottomLine: localNow.Format("15:04"),
			UpdatedAt:  now,
		}
	}

	countdown := strings.TrimSpace(ev.Countdown)
	if countdown == "" {
		countdown = FormatCountdown(ev.EndTime.Sub(now))
	}
	if runes := []rune(countdown); len(runes) > MaxDisplayChars {
		countdown = string(runes[:MaxDisplayChars])
	}

	return DisplayState{
		Mode:       DisplayModeBusy,
		TopLine:    TruncateTitle(ev.Summary, MaxDisplayChars),
		BottomLine: countdown,
		UpdatedAt:  now,
	}
}

type rgbColor struct {
	r, g, b byte
}

var (
	colorBusyTitle     = rgbColor{255, 180, 0}
	colorBusyCountdown = rgbColor{255, 60, 60}
	colorIdleDate      = rgbColor{255, 255, 255}
	colorIdleTime      = rgbColor{100, 220, 255}
)

// RenderFrame renders a DisplayState into a 72x16 RGB888 plain-encoded busybar.Frame.
func RenderFrame(state DisplayState) *busybar.Frame {
	buf := make([]byte, DisplayBufferSize)

	topColor := colorIdleDate
	bottomColor := colorIdleTime
	if state.Mode == DisplayModeBusy {
		topColor = colorBusyTitle
		bottomColor = colorBusyCountdown
	}

	drawString(buf, state.TopLine, 1, 1, topColor)
	drawString(buf, state.BottomLine, 1, 9, bottomColor)

	return &busybar.Frame{
		Screen:      busybar.ScreenFront,
		Width:       DisplayWidth,
		Height:      DisplayHeight,
		Encoding:    pb_busybar.Encoding_PLAIN,
		PixelFormat: pb_busybar.PixelFormat_RGB888,
		Data:        buf,
	}
}

const (
	glyphWidth  = 4
	glyphHeight = 6
	glyphPitch  = 5
)

func drawString(buf []byte, text string, startX, startY int, c rgbColor) {
	x := startX
	for _, r := range text {
		if x >= DisplayWidth {
			break
		}
		drawGlyph(buf, r, x, startY, c)
		x += glyphPitch
	}
}

func drawGlyph(buf []byte, r rune, startX, startY int, c rgbColor) {
	rows, ok := font4x6[r]
	if !ok {
		// Map lowercase ASCII to uppercase if not explicitly defined, or fallback to '?'.
		if r >= 'a' && r <= 'z' {
			rows, ok = font4x6[r-('a'-'A')]
		}
		if !ok {
			rows = font4x6['?']
		}
	}

	for row := 0; row < glyphHeight; row++ {
		y := startY + row
		if y < 0 || y >= DisplayHeight {
			continue
		}
		rowBits := rows[row]
		for col := 0; col < glyphWidth; col++ {
			x := startX + col
			if x < 0 || x >= DisplayWidth {
				continue
			}
			if (rowBits & (1 << (glyphWidth - 1 - col))) != 0 {
				setPixel(buf, x, y, c)
			}
		}
	}
}

func setPixel(buf []byte, x, y int, c rgbColor) {
	if x < 0 || x >= DisplayWidth || y < 0 || y >= DisplayHeight {
		return
	}
	idx := (y*DisplayWidth + x) * DisplayBytesPerPixel
	if idx < 0 || idx+2 >= len(buf) {
		return
	}
	buf[idx] = c.r
	buf[idx+1] = c.g
	buf[idx+2] = c.b
}

// font4x6 defines a 4x6 bitmap font where each byte represents a 4-bit row (bits 3..0).
var font4x6 = map[rune][6]byte{
	' ':  {0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
	'!':  {0x4, 0x4, 0x4, 0x0, 0x4, 0x0},
	'"':  {0xA, 0xA, 0x0, 0x0, 0x0, 0x0},
	'#':  {0xA, 0xF, 0xA, 0xF, 0xA, 0x0},
	'$':  {0x7, 0xC, 0x6, 0x3, 0xE, 0x0},
	'%':  {0x9, 0x2, 0x4, 0x9, 0x0, 0x0},
	'&':  {0x4, 0xA, 0x4, 0xA, 0x5, 0x0},
	'\'': {0x4, 0x4, 0x0, 0x0, 0x0, 0x0},
	'(':  {0x2, 0x4, 0x4, 0x4, 0x2, 0x0},
	')':  {0x4, 0x2, 0x2, 0x2, 0x4, 0x0},
	'*':  {0x0, 0xA, 0x4, 0xA, 0x0, 0x0},
	'+':  {0x0, 0x4, 0xE, 0x4, 0x0, 0x0},
	',':  {0x0, 0x0, 0x0, 0x4, 0x8, 0x0},
	'-':  {0x0, 0x0, 0xE, 0x0, 0x0, 0x0},
	'.':  {0x0, 0x0, 0x0, 0x0, 0x4, 0x0},
	'/':  {0x1, 0x2, 0x4, 0x8, 0x0, 0x0},
	'0':  {0x6, 0x9, 0x9, 0x9, 0x6, 0x0},
	'1':  {0x4, 0xC, 0x4, 0x4, 0xE, 0x0},
	'2':  {0xE, 0x1, 0x6, 0x8, 0xF, 0x0},
	'3':  {0xE, 0x1, 0x6, 0x1, 0xE, 0x0},
	'4':  {0x9, 0x9, 0xF, 0x1, 0x1, 0x0},
	'5':  {0xF, 0x8, 0xE, 0x1, 0xE, 0x0},
	'6':  {0x6, 0x8, 0xE, 0x9, 0x6, 0x0},
	'7':  {0xF, 0x1, 0x2, 0x4, 0x4, 0x0},
	'8':  {0x6, 0x9, 0x6, 0x9, 0x6, 0x0},
	'9':  {0x6, 0x9, 0x7, 0x1, 0x6, 0x0},
	':':  {0x0, 0x4, 0x0, 0x4, 0x0, 0x0},
	';':  {0x0, 0x4, 0x0, 0x4, 0x8, 0x0},
	'<':  {0x2, 0x4, 0x8, 0x4, 0x2, 0x0},
	'=':  {0x0, 0xE, 0x0, 0xE, 0x0, 0x0},
	'>':  {0x8, 0x4, 0x2, 0x4, 0x8, 0x0},
	'?':  {0xE, 0x1, 0x6, 0x0, 0x4, 0x0},
	'@':  {0x6, 0x9, 0xB, 0x8, 0x6, 0x0},
	'A':  {0x6, 0x9, 0xF, 0x9, 0x9, 0x0},
	'B':  {0xE, 0x9, 0xE, 0x9, 0xE, 0x0},
	'C':  {0x6, 0x9, 0x8, 0x9, 0x6, 0x0},
	'D':  {0xE, 0x9, 0x9, 0x9, 0xE, 0x0},
	'E':  {0xF, 0x8, 0xE, 0x8, 0xF, 0x0},
	'F':  {0xF, 0x8, 0xE, 0x8, 0x8, 0x0},
	'G':  {0x6, 0x8, 0xB, 0x9, 0x6, 0x0},
	'H':  {0x9, 0x9, 0xF, 0x9, 0x9, 0x0},
	'I':  {0xE, 0x4, 0x4, 0x4, 0xE, 0x0},
	'J':  {0x7, 0x2, 0x2, 0xA, 0x4, 0x0},
	'K':  {0x9, 0xA, 0xC, 0xA, 0x9, 0x0},
	'L':  {0x8, 0x8, 0x8, 0x8, 0xF, 0x0},
	'M':  {0x9, 0xF, 0xF, 0x9, 0x9, 0x0},
	'N':  {0x9, 0xD, 0xB, 0x9, 0x9, 0x0},
	'O':  {0x6, 0x9, 0x9, 0x9, 0x6, 0x0},
	'P':  {0xE, 0x9, 0xE, 0x8, 0x8, 0x0},
	'Q':  {0x6, 0x9, 0x9, 0xB, 0x7, 0x0},
	'R':  {0xE, 0x9, 0xE, 0xA, 0x9, 0x0},
	'S':  {0x7, 0x8, 0x6, 0x1, 0xE, 0x0},
	'T':  {0xE, 0x4, 0x4, 0x4, 0x4, 0x0},
	'U':  {0x9, 0x9, 0x9, 0x9, 0x6, 0x0},
	'V':  {0x9, 0x9, 0x9, 0x6, 0x6, 0x0},
	'W':  {0x9, 0x9, 0xF, 0xF, 0x9, 0x0},
	'X':  {0x9, 0x9, 0x6, 0x9, 0x9, 0x0},
	'Y':  {0x9, 0x9, 0x6, 0x4, 0x4, 0x0},
	'Z':  {0xF, 0x2, 0x4, 0x8, 0xF, 0x0},
	'[':  {0x6, 0x4, 0x4, 0x4, 0x6, 0x0},
	'\\': {0x8, 0x4, 0x2, 0x1, 0x0, 0x0},
	']':  {0x6, 0x2, 0x2, 0x2, 0x6, 0x0},
	'^':  {0x4, 0xA, 0x0, 0x0, 0x0, 0x0},
	'_':  {0x0, 0x0, 0x0, 0x0, 0xF, 0x0},
	'`':  {0x8, 0x4, 0x0, 0x0, 0x0, 0x0},
	'{':  {0x2, 0x4, 0x8, 0x4, 0x2, 0x0},
	'|':  {0x4, 0x4, 0x4, 0x4, 0x4, 0x0},
	'}':  {0x8, 0x4, 0x2, 0x4, 0x8, 0x0},
	'~':  {0x0, 0x5, 0xA, 0x0, 0x0, 0x0},
}
