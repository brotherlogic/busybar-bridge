package telemetry

import (
	"strings"
	"sync"
	"time"
)

// ConnectionStatus reports connection and telemetry metadata with JSON tags.
type ConnectionStatus struct {
	Connected          bool      `json:"connected"`
	ReconnectCount     int64     `json:"reconnect_count"`
	LastConnectedAt    time.Time `json:"last_connected_at,omitempty"`
	LastDisconnectedAt time.Time `json:"last_disconnected_at,omitempty"`
}

// EventCounters tracks cumulative counts of ingested events categorized by source type.
type EventCounters struct {
	Buttons  int64 `json:"buttons"`
	Switches int64 `json:"switches"`
	Encoders int64 `json:"encoders"`
	Other    int64 `json:"other"`
}

// ForwardingTelemetry tracks cumulative Home Assistant event delivery metrics.
type ForwardingTelemetry struct {
	TotalDispatched int64 `json:"total_dispatched"`
	TotalAcked      int64 `json:"total_acked"`
	TotalFailed     int64 `json:"total_failed"`
}

// ForwardingOutcome represents the result state of forwarding an event trace.
type ForwardingOutcome string

const (
	OutcomePending ForwardingOutcome = "pending"
	OutcomeSuccess ForwardingOutcome = "success"
	OutcomeFailure ForwardingOutcome = "failure"
)

// PushTelemetry tracks cumulative metrics for outbound frame pushes to BusyBar.
type PushTelemetry struct {
	Enabled        bool      `json:"enabled"`
	TotalAttempts  int64     `json:"total_attempts"`
	TotalSuccesses int64     `json:"total_successes"`
	TotalFailures  int64     `json:"total_failures"`
	TotalDropped   int64     `json:"total_dropped"`
	LastPushAt     time.Time `json:"last_push_at,omitempty"`
	LastLatencyMs  int64     `json:"last_latency_ms"`
	LastError      string    `json:"last_error,omitempty"`
}

// EventTrace captures point-in-time diagnostic information for an individual event.
type EventTrace struct {
	ID        int64             `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	EventType string            `json:"event_type"` // button, switch, encoder, other
	Summary   string            `json:"summary"`    // human-readable event summary
	Outcome   ForwardingOutcome `json:"outcome"`    // pending, success, failure
	Error     string            `json:"error,omitempty"`
	LatencyMs int64             `json:"latency_ms,omitempty"`
}

// Snapshot provides an atomic, point-in-time copy of store telemetry state.
type Snapshot struct {
	StartTime    time.Time
	Uptime       time.Duration
	Connection   ConnectionStatus
	Counters     EventCounters
	Forwarding   ForwardingTelemetry
	Push         PushTelemetry
	TotalEvents  int64
	RecentEvents []EventTrace // Oldest to newest (up to 10)
}

// Store maintains thread-safe telemetry state and an event trace circular ring buffer.
type Store struct {
	mu              sync.RWMutex
	startTime       time.Time
	connection      ConnectionStatus
	counters        EventCounters
	forwarding      ForwardingTelemetry
	push            PushTelemetry
	ringBuffer      []EventTrace
	nextID          int64
	totalEvents     int64
	maxRingCapacity int
	dropCount       int64
}

// NewStore initializes a Store with current start time and a 10-event capacity ring buffer.
func NewStore() *Store {
	return &Store{
		startTime:       time.Now(),
		maxRingCapacity: 10,
		ringBuffer:      make([]EventTrace, 0, 10),
	}
}

// SetConnected updates the connection status and the corresponding timestamp.
func (s *Store) SetConnected(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connection.Connected = connected
	if connected {
		s.connection.LastConnectedAt = time.Now()
	} else {
		s.connection.LastDisconnectedAt = time.Now()
	}
}

// IncrementReconnectCount increments the cumulative count of reconnection attempts.
func (s *Store) IncrementReconnectCount() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connection.ReconnectCount++
}

// RecordEvent appends an event trace to the ring buffer (evicting the oldest if capacity is reached)
// and increments the corresponding event category counter. Returns the generated unique trace ID.
func (s *Store) RecordEvent(eventType string, summary string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := s.nextID
	s.totalEvents++

	switch strings.ToLower(eventType) {
	case "button", "buttons":
		s.counters.Buttons++
	case "switch", "switches":
		s.counters.Switches++
	case "encoder", "encoders":
		s.counters.Encoders++
	default:
		s.counters.Other++
	}

	trace := EventTrace{
		ID:        id,
		Timestamp: time.Now(),
		EventType: eventType,
		Summary:   summary,
		Outcome:   OutcomePending,
	}

	if len(s.ringBuffer) >= s.maxRingCapacity {
		s.ringBuffer = append(s.ringBuffer[1:], trace)
	} else {
		s.ringBuffer = append(s.ringBuffer, trace)
	}

	return id
}

// RecordForwardOutcome updates an existing trace by ID and increments forwarding telemetry counters.
func (s *Store) RecordForwardOutcome(id int64, outcome ForwardingOutcome, latency time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.forwarding.TotalDispatched++
	switch outcome {
	case OutcomeSuccess:
		s.forwarding.TotalAcked++
	case OutcomeFailure:
		s.forwarding.TotalFailed++
	}

	for i := range s.ringBuffer {
		if s.ringBuffer[i].ID == id {
			s.ringBuffer[i].Outcome = outcome
			s.ringBuffer[i].LatencyMs = latency.Milliseconds()
			if err != nil {
				s.ringBuffer[i].Error = err.Error()
			}
			break
		}
	}
}

// IsConnected returns the current connection state.
func (s *Store) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.connection.Connected
}

// Snapshot returns an atomic, point-in-time copy of the store state.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	recent := make([]EventTrace, len(s.ringBuffer))
	copy(recent, s.ringBuffer)

	return Snapshot{
		StartTime:    s.startTime,
		Uptime:       time.Since(s.startTime),
		Connection:   s.connection,
		Counters:     s.counters,
		Forwarding:   s.forwarding,
		Push:         s.push,
		TotalEvents:  s.totalEvents,
		RecentEvents: recent,
	}
}

// SetPushEnabled sets whether outbound push is currently enabled.
func (s *Store) SetPushEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.push.Enabled = enabled
}

// RecordPushAttempt increments the cumulative counter of outbound push attempts.
func (s *Store) RecordPushAttempt() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.push.TotalAttempts++
}

// RecordPushResult records the result of an outbound push delivery, updating successes or failures,
// delivery latency, timestamp, and any error encountered.
func (s *Store) RecordPushResult(success bool, latency time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.push.LastPushAt = time.Now()
	s.push.LastLatencyMs = latency.Milliseconds()

	if success {
		s.push.TotalSuccesses++
		s.push.LastError = ""
	} else {
		s.push.TotalFailures++
		if err != nil {
			s.push.LastError = err.Error()
		}
	}
}

// RecordPushDrop increments the cumulative counter of dropped or superseded outbound push frames.
func (s *Store) RecordPushDrop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.push.TotalDropped++
}

// RecordDrop records an event drop reason in the telemetry store.
func (s *Store) RecordDrop(reason ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropCount++
}

// DropCount returns the total number of dropped events.
func (s *Store) DropCount() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dropCount
}
