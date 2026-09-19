package server

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"
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

type mockSnapshot struct {
	StartTime    time.Time
	Uptime       time.Duration
	Connection   mockConnectionStatus
	Counters     mockEventCounters
	Forwarding   mockForwardingTelemetry
	TotalEvents  int64
	RecentEvents []mockEventTrace
	Calendar     CalendarStatus
}

func parseStatusTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.ParseFiles("templates/status.html")
	if err != nil {
		t.Fatalf("failed to parse templates/status.html: %v", err)
	}
	return tmpl
}

func renderTemplate(t *testing.T, tmpl *template.Template, data mockSnapshot) string {
	t.Helper()
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

