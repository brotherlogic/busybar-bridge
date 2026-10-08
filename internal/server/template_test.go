package server

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/calendar"
	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
)

// Mock data structures mirroring the telemetry snapshot schema
type mockConnectionStatus struct {
	Connected          bool
	ReconnectCount     int64
	LastConnectedAt    time.Time
	LastDisconnectedAt time.Time
}

type mockEventCounters struct {
	Buttons  int64
	Switches int64
	Encoders int64
	Other    int64
}

type mockForwardingTelemetry struct {
	TotalDispatched int64
	TotalAcked      int64
	TotalFailed     int64
}

type mockEventTrace struct {
	ID        int64
	Timestamp time.Time
	EventType string
	Summary   string
	Outcome   string
	Error     string
	LatencyMs int64
}

type mockNestedSnapshot struct {
	CalendarSync telemetry.CalendarSyncTelemetry
}

type mockSnapshot struct {
	StartTime    time.Time
	Uptime       time.Duration
	Connection   mockConnectionStatus
	Counters     mockEventCounters
	Forwarding   mockForwardingTelemetry
	Push         telemetry.PushTelemetry
	CalendarSync telemetry.CalendarSyncTelemetry
	Snapshot     mockNestedSnapshot
	TotalEvents  int64
	RecentEvents []mockEventTrace
	Calendar     CalendarStatus
	ActiveEvent  calendar.ActiveEvent
}

func parseStatusTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.ParseFiles("templates/status.html")
	if err != nil {
		t.Fatalf("failed to parse templates/status.html: %v", err)
	}
	return tmpl
}

func renderTemplate(t *testing.T, tmpl *template.Template, data any) string {
	t.Helper()
	if ms, ok := data.(mockSnapshot); ok {
		if ms.Snapshot.CalendarSync == (telemetry.CalendarSyncTelemetry{}) && ms.CalendarSync != (telemetry.CalendarSyncTelemetry{}) {
			ms.Snapshot.CalendarSync = ms.CalendarSync
		}
		data = ms
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}
	return buf.String()
}

func TestStatusTemplate_MetaRefreshAndNoExternalDependencies(t *testing.T) {
	tmpl := parseStatusTemplate(t)
	rendered := renderTemplate(t, tmpl, mockSnapshot{})

	// Verify meta refresh tag
	if !strings.Contains(rendered, `<meta http-equiv="refresh" content="5">`) {
		t.Errorf("template missing required meta refresh tag: <meta http-equiv=\"refresh\" content=\"5\">")
	}

	// Verify style tag is embedded
	if !strings.Contains(rendered, "<style>") || !strings.Contains(rendered, "</style>") {
		t.Errorf("template missing embedded <style> block")
	}

	// Verify no external CSS or CDN links
	if strings.Contains(rendered, `<link rel="stylesheet"`) {
		t.Errorf("template should not contain external stylesheet link tags")
	}
	if strings.Contains(rendered, "http://") || strings.Contains(rendered, "https://") {
		t.Errorf("template contains external URL dependencies")
	}
}

func TestStatusTemplate_ConnectionStateBadges(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	// Test Connected state
	connSnapshot := mockSnapshot{
		Connection: mockConnectionStatus{Connected: true},
	}
	renderedConnected := renderTemplate(t, tmpl, connSnapshot)
	if !strings.Contains(renderedConnected, "Connected") {
		t.Errorf("expected rendered HTML to contain 'Connected'")
	}
	if !strings.Contains(renderedConnected, "badge-success") {
		t.Errorf("expected connected badge to contain class 'badge-success'")
	}

	// Test Disconnected state
	disconnSnapshot := mockSnapshot{
		Connection: mockConnectionStatus{Connected: false},
	}
	renderedDisconnected := renderTemplate(t, tmpl, disconnSnapshot)
	if !strings.Contains(renderedDisconnected, "Disconnected") {
		t.Errorf("expected rendered HTML to contain 'Disconnected'")
	}
	if !strings.Contains(renderedDisconnected, "badge-failure") {
		t.Errorf("expected disconnected badge to contain class 'badge-failure'")
	}
}

func TestStatusTemplate_MetricSummaryCards(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	snapshot := mockSnapshot{
		Uptime:      12 * time.Minute,
		TotalEvents: 142,
		Forwarding: mockForwardingTelemetry{
			TotalDispatched: 140,
			TotalAcked:      135,
			TotalFailed:     5,
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	// Summary card labels
	for _, label := range []string{"Uptime", "Total Events", "Successful Deliveries", "Failures"} {
		if !strings.Contains(rendered, label) {
			t.Errorf("expected summary cards to include label %q", label)
		}
	}

	// Metric values
	if !strings.Contains(rendered, "142") {
		t.Errorf("expected rendered HTML to contain TotalEvents value '142'")
	}
	if !strings.Contains(rendered, "135") {
		t.Errorf("expected rendered HTML to contain Successful Deliveries value '135'")
	}
	if !strings.Contains(rendered, "5") {
		t.Errorf("expected rendered HTML to contain Failures value '5'")
	}
}

func TestStatusTemplate_RecentEventsTableAndBadges(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	ts := time.Date(2026, 9, 12, 18, 30, 0, 0, time.UTC)
	snapshot := mockSnapshot{
		RecentEvents: []mockEventTrace{
			{
				ID:        101,
				Timestamp: ts,
				EventType: "button",
				Summary:   "button: ok (press)",
				Outcome:   "success",
				LatencyMs: 15,
				Error:     "",
			},
			{
				ID:        102,
				Timestamp: ts.Add(1 * time.Second),
				EventType: "switch",
				Summary:   "switch: toggle (on)",
				Outcome:   "failure",
				LatencyMs: 42,
				Error:     "connection timeout",
			},
			{
				ID:        103,
				Timestamp: ts.Add(2 * time.Second),
				EventType: "encoder",
				Summary:   "encoder: rotate (+1)",
				Outcome:   "pending",
				LatencyMs: 0,
				Error:     "",
			},
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	// Table headers
	for _, header := range []string{"ID", "Timestamp", "Event Type", "Summary", "Outcome", "Latency", "Error"} {
		if !strings.Contains(rendered, header) {
			t.Errorf("expected table to include column header %q", header)
		}
	}

	// Event 101 content and badge-success
	if !strings.Contains(rendered, "101") || !strings.Contains(rendered, "button: ok (press)") {
		t.Errorf("expected event 101 trace to be rendered")
	}
	if !strings.Contains(rendered, "badge-success") {
		t.Errorf("expected badge-success outcome badge")
	}

	// Event 102 content and badge-failure with error message
	if !strings.Contains(rendered, "102") || !strings.Contains(rendered, "connection timeout") {
		t.Errorf("expected event 102 trace and error message to be rendered")
	}
	if !strings.Contains(rendered, "badge-failure") {
		t.Errorf("expected badge-failure outcome badge")
	}

	// Event 103 content and badge-pending
	if !strings.Contains(rendered, "103") || !strings.Contains(rendered, "badge-pending") {
		t.Errorf("expected event 103 and badge-pending outcome badge")
	}
}

func TestStatusTemplate_EmptyState(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	snapshot := mockSnapshot{
		RecentEvents: []mockEventTrace{},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	if !strings.Contains(rendered, "No events received yet") {
		t.Errorf("expected empty state message 'No events received yet' when RecentEvents is empty")
	}
}

func TestStatusTemplate_CalendarOAuthUnconfigured(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	snapshot := mockSnapshot{
		Calendar: CalendarStatus{
			Configured: false,
			Bound:      false,
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	if !strings.Contains(rendered, "Google Calendar") {
		t.Errorf("expected Google Calendar card in rendered output")
	}
	if !strings.Contains(rendered, "OAUTH UNCONFIGURED") {
		t.Errorf("expected 'OAUTH UNCONFIGURED' badge when OAuth is unconfigured")
	}
	if !strings.Contains(rendered, "badge-pending") {
		t.Errorf("expected yellow badge-pending class for unconfigured state")
	}
	if !strings.Contains(strings.ToLower(rendered), "client id") || !strings.Contains(strings.ToLower(rendered), "secret") {
		t.Errorf("expected guidance on configuring OAuth client ID and secret")
	}
	if strings.Contains(rendered, "/oauth/google/login") {
		t.Errorf("connect link should not be rendered when OAuth is unconfigured")
	}
}

func TestStatusTemplate_CalendarConfiguredUnlinked(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	snapshot := mockSnapshot{
		Calendar: CalendarStatus{
			Configured: true,
			Bound:      false,
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	if !strings.Contains(rendered, "Google Calendar") {
		t.Errorf("expected Google Calendar card in rendered output")
	}
	if !strings.Contains(rendered, "Connect Google Calendar") {
		t.Errorf("expected 'Connect Google Calendar' button when configured and unlinked")
	}
	if !strings.Contains(rendered, "/oauth/google/login") {
		t.Errorf("expected link to '/oauth/google/login' when configured and unlinked")
	}
	if strings.Contains(rendered, "OAUTH UNCONFIGURED") {
		t.Errorf("did not expect 'OAUTH UNCONFIGURED' badge when OAuth is configured")
	}
	if strings.Contains(rendered, "LOCKED / IMMUTABLE") {
		t.Errorf("did not expect 'LOCKED / IMMUTABLE' badge when not bound")
	}
}

func TestStatusTemplate_CalendarBoundImmutable(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	snapshot := mockSnapshot{
		Calendar: CalendarStatus{
			Configured: true,
			Bound:      true,
			Email:      "operator@example.com",
			LinkedAt:   "2026-09-19T12:00:00Z",
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	if !strings.Contains(rendered, "Google Calendar") {
		t.Errorf("expected Google Calendar card in rendered output")
	}
	if !strings.Contains(rendered, "LOCKED / IMMUTABLE") {
		t.Errorf("expected 'LOCKED / IMMUTABLE' badge when bound")
	}
	if !strings.Contains(rendered, "badge-success") {
		t.Errorf("expected badge-success class for locked/immutable badge")
	}
	if !strings.Contains(rendered, "operator@example.com") {
		t.Errorf("expected authenticated email 'operator@example.com' to be rendered")
	}
	if !strings.Contains(rendered, "2026-09-19T12:00:00Z") {
		t.Errorf("expected linked date '2026-09-19T12:00:00Z' to be rendered")
	}
	if !strings.Contains(strings.ToLower(rendered), "permanently locked") {
		t.Errorf("expected note that calendar access is permanently locked")
	}
	if strings.Contains(rendered, "/oauth/google/login") || strings.Contains(rendered, "Connect Google Calendar") {
		t.Errorf("connect button should be omitted when calendar is bound and immutable")
	}
}

func TestStatusTemplate_FlashAlerts(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	// No flash messages
	renderedNoAlerts := renderTemplate(t, tmpl, mockSnapshot{})
	if strings.Contains(renderedNoAlerts, `role="alert"`) || strings.Contains(renderedNoAlerts, `class="alert `) {
		t.Errorf("expected no alert banners when FlashError and FlashSuccess are empty")
	}

	// Error flash
	snapshotError := mockSnapshot{
		Calendar: CalendarStatus{
			FlashError: "OAuth authorization was canceled by user",
		},
	}
	renderedError := renderTemplate(t, tmpl, snapshotError)
	if !strings.Contains(renderedError, `class="alert alert-error"`) {
		t.Errorf("expected alert-error class for FlashError")
	}
	if !strings.Contains(renderedError, "OAuth authorization was canceled by user") {
		t.Errorf("expected FlashError text to be rendered")
	}

	// Success flash
	snapshotSuccess := mockSnapshot{
		Calendar: CalendarStatus{
			FlashSuccess: "Google Calendar linked successfully",
		},
	}
	renderedSuccess := renderTemplate(t, tmpl, snapshotSuccess)
	if !strings.Contains(renderedSuccess, `class="alert alert-success"`) {
		t.Errorf("expected alert-success class for FlashSuccess")
	}
	if !strings.Contains(renderedSuccess, "Google Calendar linked successfully") {
		t.Errorf("expected FlashSuccess text to be rendered")
	}
}

func TestStatusTemplate_PushActiveBadge(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	snapshot := mockSnapshot{
		Push: telemetry.PushTelemetry{
			Enabled: true,
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	if !strings.Contains(rendered, "Outbound Push Transport") {
		t.Errorf("expected rendered HTML to contain 'Outbound Push Transport'")
	}
	if !strings.Contains(rendered, `<span class="badge badge-success">ACTIVE</span>`) {
		t.Errorf("expected rendered HTML to contain '<span class=\"badge badge-success\">ACTIVE</span>'")
	}
	if strings.Contains(rendered, "DISABLED (OPT-IN)") {
		t.Errorf("did not expect 'DISABLED (OPT-IN)' when push is enabled")
	}
}

func TestStatusTemplate_PushDisabledBadge(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	snapshot := mockSnapshot{
		Push: telemetry.PushTelemetry{
			Enabled: false,
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	if !strings.Contains(rendered, "Outbound Push Transport") {
		t.Errorf("expected rendered HTML to contain 'Outbound Push Transport'")
	}
	if !strings.Contains(rendered, `<span class="badge badge-neutral">DISABLED (OPT-IN)</span>`) {
		t.Errorf("expected rendered HTML to contain '<span class=\"badge badge-neutral\">DISABLED (OPT-IN)</span>'")
	}
	if strings.Contains(rendered, ">ACTIVE</span>") {
		t.Errorf("did not expect 'ACTIVE' badge when push is disabled")
	}
}

func TestStatusTemplate_PushCountersAndErrorBanner(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	snapshot := mockSnapshot{
		Push: telemetry.PushTelemetry{
			Enabled:        true,
			TotalAttempts:  150,
			TotalSuccesses: 140,
			TotalFailures:  7,
			TotalDropped:   3,
			LastError:      "connection refused: 192.168.1.50:80",
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	// Counters and labels
	for _, label := range []string{"Total Attempts", "Delivered", "Failed", "Superseded / Dropped"} {
		if !strings.Contains(rendered, label) {
			t.Errorf("expected rendered HTML to contain label %q", label)
		}
	}

	// Counter values with their respective styling classes
	if !strings.Contains(rendered, "150") {
		t.Errorf("expected rendered HTML to contain Total Attempts value '150'")
	}
	if !strings.Contains(rendered, "140") || !strings.Contains(rendered, "text-success") {
		t.Errorf("expected Delivered value '140' with 'text-success' class")
	}
	if !strings.Contains(rendered, "7") || !strings.Contains(rendered, "text-danger") {
		t.Errorf("expected Failed value '7' with 'text-danger' class")
	}
	if !strings.Contains(rendered, "3") || !strings.Contains(rendered, "text-warning") {
		t.Errorf("expected Superseded / Dropped value '3' with 'text-warning' class")
	}

	// Error banner
	expectedBanner := `<div class="error-banner">Last Error: connection refused: 192.168.1.50:80</div>`
	if !strings.Contains(rendered, expectedBanner) {
		t.Errorf("expected error banner %q in rendered HTML", expectedBanner)
	}
}

func TestStatusTemplate_PushErrorBanner_OmittedWhenEmpty(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	snapshot := mockSnapshot{
		Push: telemetry.PushTelemetry{
			Enabled:        true,
			TotalAttempts:  20,
			TotalSuccesses: 20,
			TotalFailures:  0,
			TotalDropped:   0,
			LastError:      "",
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	if strings.Contains(rendered, `<div class="error-banner">`) || strings.Contains(rendered, "Last Error:") {
		t.Errorf("error banner should not be rendered when LastError is empty")
	}
}

func TestStatusTemplate_PushCSSClassesPresent(t *testing.T) {
	tmpl := parseStatusTemplate(t)
	rendered := renderTemplate(t, tmpl, mockSnapshot{})

	for _, class := range []string{"badge-neutral", "text-success", "text-danger", "text-warning", "error-banner"} {
		if !strings.Contains(rendered, "."+class) {
			t.Errorf("expected CSS stylesheet to define class %q", "."+class)
		}
	}
}

func TestStatusTemplate_ActiveEvent_IdleBadge(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	// Case 1: Idle is explicitly true
	snapshotIdle := mockSnapshot{
		ActiveEvent: calendar.ActiveEvent{
			Idle: true,
		},
	}
	renderedIdle := renderTemplate(t, tmpl, snapshotIdle)
	if !strings.Contains(renderedIdle, `<span class="badge badge-neutral">IDLE</span>`) {
		t.Errorf("expected '<span class=\"badge badge-neutral\">IDLE</span>' when active meeting is idle")
	}
	if strings.Contains(renderedIdle, "IN MEETING:") {
		t.Errorf("did not expect 'IN MEETING:' when active meeting is idle")
	}

	// Case 2: Summary is empty (even if Idle was false)
	snapshotEmptySummary := mockSnapshot{
		ActiveEvent: calendar.ActiveEvent{
			Idle:    false,
			Summary: "",
		},
	}
	renderedEmpty := renderTemplate(t, tmpl, snapshotEmptySummary)
	if !strings.Contains(renderedEmpty, `<span class="badge badge-neutral">IDLE</span>`) {
		t.Errorf("expected '<span class=\"badge badge-neutral\">IDLE</span>' when summary is empty")
	}
	if strings.Contains(renderedEmpty, "IN MEETING:") {
		t.Errorf("did not expect 'IN MEETING:' when summary is empty")
	}
}

func TestStatusTemplate_ActiveEvent_InMeetingBadgeAndDetails(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	startTime := time.Date(2026, 10, 8, 14, 0, 0, 0, time.Local)
	endTime := time.Date(2026, 10, 8, 15, 0, 0, 0, time.Local)

	snapshot := mockSnapshot{
		ActiveEvent: calendar.ActiveEvent{
			Summary:   "Sprint Planning",
			StartTime: startTime,
			EndTime:   endTime,
			Countdown: "00:45",
			Idle:      false,
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	// In meeting warning badge
	expectedBadge := `<span class="badge badge-warning">IN MEETING: 00:45</span>`
	if !strings.Contains(rendered, expectedBadge) {
		t.Errorf("expected badge %q in rendered output", expectedBadge)
	}
	if strings.Contains(rendered, `<span class="badge badge-neutral">IDLE</span>`) {
		t.Errorf("did not expect IDLE badge when meeting is active")
	}

	// Active meeting summary
	if !strings.Contains(rendered, "Sprint Planning") {
		t.Errorf("expected active meeting summary 'Sprint Planning' in rendered output")
	}

	// Active meeting countdown
	if !strings.Contains(rendered, "00:45") {
		t.Errorf("expected active meeting countdown '00:45' in rendered output")
	}

	// Active meeting start and end times
	startStr := startTime.Local().Format("15:04")
	endStr := endTime.Local().Format("15:04")
	if !strings.Contains(rendered, startStr) {
		t.Errorf("expected scheduled start time %q in rendered output", startStr)
	}
	if !strings.Contains(rendered, endStr) {
		t.Errorf("expected scheduled end time %q in rendered output", endStr)
	}
}

func TestStatusTemplate_CalendarSync_TelemetryCountersAndErrorBanner(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	lastPoll := time.Date(2026, 10, 8, 14, 15, 30, 0, time.UTC)
	snapshot := mockSnapshot{
		Snapshot: mockNestedSnapshot{
			CalendarSync: telemetry.CalendarSyncTelemetry{
				Enabled:      true,
				TotalPolls:   128,
				SuccessPolls: 120,
				FailedPolls:  8,
				LastPollAt:   lastPoll,
				LastError:    "Google API 503 Backend Error",
			},
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	// Labels
	for _, label := range []string{"Total Polls", "Successful Polls", "Failed Polls", "Last Poll Time"} {
		if !strings.Contains(rendered, label) {
			t.Errorf("expected rendered HTML to contain label %q", label)
		}
	}

	// Values and classes
	if !strings.Contains(rendered, "128") {
		t.Errorf("expected rendered HTML to contain Total Polls '128'")
	}
	if !strings.Contains(rendered, "120") || !strings.Contains(rendered, "text-success") {
		t.Errorf("expected Successful Polls '120' with 'text-success' class")
	}
	if !strings.Contains(rendered, "8") || !strings.Contains(rendered, "text-danger") {
		t.Errorf("expected Failed Polls '8' with 'text-danger' class")
	}
	if !strings.Contains(rendered, lastPoll.String()) && !strings.Contains(rendered, lastPoll.Local().Format("15:04")) && !strings.Contains(rendered, "2026-10-08") {
		t.Errorf("expected Last Poll Time to be rendered")
	}

	// Error banner
	expectedBanner := `<div class="error-banner">Last Error: Google API 503 Backend Error</div>`
	if !strings.Contains(rendered, expectedBanner) {
		t.Errorf("expected error banner %q in rendered output", expectedBanner)
	}
}

func TestStatusTemplate_CalendarSync_ErrorBanner_OmittedWhenEmpty(t *testing.T) {
	tmpl := parseStatusTemplate(t)

	snapshot := mockSnapshot{
		Snapshot: mockNestedSnapshot{
			CalendarSync: telemetry.CalendarSyncTelemetry{
				Enabled:      true,
				TotalPolls:   10,
				SuccessPolls: 10,
				FailedPolls:  0,
				LastError:    "",
			},
		},
	}
	rendered := renderTemplate(t, tmpl, snapshot)

	if strings.Contains(rendered, "Last Error:") {
		t.Errorf("error banner should not be rendered when CalendarSync.LastError is empty")
	}
}

func TestStatusTemplate_BadgeWarningCSSClassPresent(t *testing.T) {
	tmpl := parseStatusTemplate(t)
	rendered := renderTemplate(t, tmpl, mockSnapshot{})

	if !strings.Contains(rendered, ".badge-warning") {
		t.Errorf("expected CSS stylesheet to define class '.badge-warning'")
	}
}

