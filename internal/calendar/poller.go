package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
	"golang.org/x/oauth2"
)

const (
	// DefaultCalendarBaseURL is the Google Calendar v3 API calendars endpoint base URL.
	DefaultCalendarBaseURL = "https://www.googleapis.com/calendar/v3/calendars"

	// DefaultPollInterval is the default duration between Google Calendar API polls.
	DefaultPollInterval = 1 * time.Minute
)

// ActiveEvent describes the currently ongoing calendar event and countdown state.
type ActiveEvent struct {
	Summary   string    `json:"summary,omitempty"`
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`
	Countdown string    `json:"countdown,omitempty"`
	Idle      bool      `json:"idle"`
}

// EventTracker defines the queryable interface for active event state.
type EventTracker interface {
	GetCurrentEvent() ActiveEvent
}

// PollerConfig defines the configuration parameters for the Google Calendar background poller.
type PollerConfig struct {
	Enabled         bool
	PollInterval    time.Duration
	Store           *Store
	Manager         *Manager
	Telemetry       *telemetry.Store
	CalendarBaseURL string
	HTTPClient      *http.Client
	Now             func() time.Time
}

// Poller manages background calendar polling, filtering, overlap resolution,
// countdown calculation, and telemetry recording.
type Poller struct {
	cfg        PollerConfig
	store      *Store
	manager    *Manager
	telemetry  *telemetry.Store
	httpClient *http.Client
	now        func() time.Time

	mu          sync.RWMutex
	activeEvent ActiveEvent

	started   atomic.Bool
	closed    atomic.Bool
	closeOnce sync.Once
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewPoller initializes a new Poller using the provided configuration.
func NewPoller(cfg PollerConfig) (*Poller, error) {
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	cfg.PollInterval = interval

	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	if cfg.Telemetry != nil {
		cfg.Telemetry.SetCalendarSyncEnabled(cfg.Enabled)
	}

	p := &Poller{
		cfg:         cfg,
		store:       cfg.Store,
		manager:     cfg.Manager,
		telemetry:   cfg.Telemetry,
		httpClient:  cfg.HTTPClient,
		now:         nowFn,
		activeEvent: ActiveEvent{Idle: true},
	}

	return p, nil
}

// FormatCountdown formats a remaining duration as ceiling-rounded HH:MM without truncating hours >= 24h.
// Durations <= 0 return "00:00".
func FormatCountdown(remaining time.Duration) string {
	if remaining <= 0 {
		return "00:00"
	}

	// Ceiling round to the nearest whole minute
	totalMinutes := int64((remaining + time.Minute - time.Nanosecond) / time.Minute)
	hours := totalMinutes / 60
	minutes := totalMinutes % 60

	return fmt.Sprintf("%02d:%02d", hours, minutes)
}

// GetCurrentEvent returns the active calendar event with dynamically computed countdown,
// or an idle event if no meeting is ongoing or credentials/store are inactive.
func (p *Poller) GetCurrentEvent() ActiveEvent {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.activeEvent.Idle || p.activeEvent.EndTime.IsZero() {
		return ActiveEvent{Idle: true}
	}

	now := p.now()
	if !now.Before(p.activeEvent.EndTime) {
		return ActiveEvent{Idle: true}
	}

	if now.Before(p.activeEvent.StartTime) {
		return ActiveEvent{Idle: true}
	}

	ev := p.activeEvent
	ev.Countdown = FormatCountdown(ev.EndTime.Sub(now))
	ev.Idle = false
	return ev
}

// Start launches the background polling worker loop.
func (p *Poller) Start(ctx context.Context) error {
	if p.closed.Load() {
		return errors.New("poller is closed")
	}
	if p.started.Swap(true) {
		return errors.New("poller already started")
	}

	workerCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.wg.Add(1)
	go p.loop(workerCtx)

	return nil
}

// Close terminates the background polling worker loop and waits for completion.
func (p *Poller) Close() error {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		if p.cancel != nil {
			p.cancel()
		}
		p.wg.Wait()
	})
	return nil
}

func (p *Poller) loop(ctx context.Context) {
	defer p.wg.Done()

	// Initial immediate poll on startup
	_ = p.Poll(ctx)

	interval := p.cfg.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = p.Poll(ctx)
		}
	}
}

// Poll queries the Google Calendar API for primary events within [now - 12h, now + 24h],
// filters out ineligible events, resolves overlapping events, and updates active event state.
func (p *Poller) Poll(ctx context.Context) error {
	now := p.now()

	// Safe dormant idle check when disabled or unbound
	if !p.cfg.Enabled || p.store == nil || !p.store.IsBound() {
		p.mu.Lock()
		p.activeEvent = ActiveEvent{Idle: true}
		p.mu.Unlock()
		return nil
	}

	binding, ok := p.store.Get()
	if !ok || binding == nil {
		p.mu.Lock()
		p.activeEvent = ActiveEvent{Idle: true}
		p.mu.Unlock()
		return nil
	}

	client := p.httpClient
	if client == nil {
		if p.manager != nil && p.manager.oauthConfig != nil {
			token := &oauth2.Token{
				AccessToken:  binding.AccessToken,
				RefreshToken: binding.RefreshToken,
				TokenType:    "Bearer",
			}
			if binding.TokenExpiryUnix > 0 {
				token.Expiry = time.Unix(binding.TokenExpiryUnix, 0)
			}
			ts := oauth2.ReuseTokenSource(token, p.manager.oauthConfig.TokenSource(ctx, token))
			client = oauth2.NewClient(ctx, ts)
		} else {
			client = http.DefaultClient
		}
	}

	calendarID := binding.CalendarId
	if strings.TrimSpace(calendarID) == "" {
		calendarID = "primary"
	}

	baseURL := p.cfg.CalendarBaseURL
	if baseURL == "" {
		baseURL = DefaultCalendarBaseURL
	}

	timeMin := now.Add(-12 * time.Hour).UTC().Format(time.RFC3339)
	timeMax := now.Add(24 * time.Hour).UTC().Format(time.RFC3339)

	reqURL := fmt.Sprintf("%s/%s/events?timeMin=%s&timeMax=%s&singleEvents=true",
		strings.TrimRight(baseURL, "/"),
		url.PathEscape(calendarID),
		url.QueryEscape(timeMin),
		url.QueryEscape(timeMax),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		p.handlePollFailure(now, fmt.Errorf("failed to create calendar request: %w", err))
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		p.handlePollFailure(now, fmt.Errorf("failed to execute calendar request: %w", err))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		pollErr := fmt.Errorf("calendar API returned status %d: %s", resp.StatusCode, string(body))
		p.handlePollFailure(now, pollErr)
		return pollErr
	}

	var eventList calendarEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&eventList); err != nil {
		p.handlePollFailure(now, fmt.Errorf("failed to decode calendar response: %w", err))
		return err
	}

	var activeEvents []ActiveEvent
	for _, item := range eventList.Items {
		// Exclude cancelled events
		if strings.EqualFold(item.Status, "cancelled") {
			continue
		}
		// Exclude transparent (free) events
		if strings.EqualFold(item.Transparency, "transparent") {
			continue
		}
		// Exclude all-day events (date-only start/end)
		if item.Start.DateTime == "" || item.End.DateTime == "" {
			continue
		}

		startTime, err := time.Parse(time.RFC3339, item.Start.DateTime)
		if err != nil {
			continue
		}
		endTime, err := time.Parse(time.RFC3339, item.End.DateTime)
		if err != nil {
			continue
		}

		// Exclude declined invitations
		if isDeclined(item.Attendees, binding.Email) {
			continue
		}

		// Active if now >= start_time and now < end_time
		if (now.Equal(startTime) || now.After(startTime)) && now.Before(endTime) {
			activeEvents = append(activeEvents, ActiveEvent{
				Summary:   item.Summary,
				StartTime: startTime,
				EndTime:   endTime,
				Countdown: FormatCountdown(endTime.Sub(now)),
				Idle:      false,
			})
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if len(activeEvents) == 0 {
		p.activeEvent = ActiveEvent{Idle: true}
		if p.telemetry != nil {
			p.telemetry.RecordCalendarPoll(true, "", nil)
		}
		return nil
	}

	// Overlap resolution and priority:
	// 1. Prioritize earliest end_time
	// 2. Tie-breaker: latest start_time (most recently started)
	sort.Slice(activeEvents, func(i, j int) bool {
		if !activeEvents[i].EndTime.Equal(activeEvents[j].EndTime) {
			return activeEvents[i].EndTime.Before(activeEvents[j].EndTime)
		}
		if !activeEvents[i].StartTime.Equal(activeEvents[j].StartTime) {
			return activeEvents[i].StartTime.After(activeEvents[j].StartTime)
		}
		return activeEvents[i].Summary < activeEvents[j].Summary
	})

	winner := activeEvents[0]
	p.activeEvent = winner

	if p.telemetry != nil {
		p.telemetry.RecordCalendarPoll(true, winner.Summary, nil)
	}

	return nil
}

func (p *Poller) handlePollFailure(now time.Time, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	activeSummary := ""
	if !p.activeEvent.Idle && !p.activeEvent.EndTime.IsZero() {
		if !now.Before(p.activeEvent.EndTime) {
			// Scheduled end time has elapsed during outage -> transition cleanly to idle
			p.activeEvent = ActiveEvent{Idle: true}
		} else {
			// Retain cached active event and continue local countdown
			p.activeEvent.Countdown = FormatCountdown(p.activeEvent.EndTime.Sub(now))
			activeSummary = p.activeEvent.Summary
		}
	}

	if p.telemetry != nil {
		p.telemetry.RecordCalendarPoll(false, activeSummary, err)
	}
}

func isDeclined(attendees []calendarAttendee, userEmail string) bool {
	if len(attendees) == 0 {
		return false
	}

	hasSelfOrEmail := false
	for _, a := range attendees {
		if a.Self || (userEmail != "" && strings.EqualFold(a.Email, userEmail)) {
			hasSelfOrEmail = true
			if strings.EqualFold(a.ResponseStatus, "declined") {
				return true
			}
		}
	}

	if !hasSelfOrEmail {
		for _, a := range attendees {
			if strings.EqualFold(a.ResponseStatus, "declined") {
				return true
			}
		}
	}

	return false
}

type calendarEventsResponse struct {
	Items []calendarEventItem `json:"items"`
}

type calendarEventItem struct {
	ID           string             `json:"id"`
	Summary      string             `json:"summary"`
	Status       string             `json:"status"`
	Transparency string             `json:"transparency"`
	Start        calendarEventTime  `json:"start"`
	End          calendarEventTime  `json:"end"`
	Attendees    []calendarAttendee `json:"attendees"`
}

type calendarEventTime struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
	TimeZone string `json:"timeZone"`
}

type calendarAttendee struct {
	Email          string `json:"email"`
	Self           bool   `json:"self"`
	ResponseStatus string `json:"responseStatus"`
}
