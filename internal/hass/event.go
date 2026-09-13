package hass

import (
	"encoding/json"
	"fmt"
	"time"
)

// EventType defines the string enumeration of supported Home Assistant event categories.
type EventType string

const (
	// EventTypeButton represents a Busy Bar button interaction event.
	EventTypeButton EventType = "button"
	// EventTypeSwitch represents a Busy Bar rotary/toggle switch position event.
	EventTypeSwitch EventType = "switch"
	// EventTypeEncoder represents a Busy Bar rotary encoder increment/decrement event.
	EventTypeEncoder EventType = "encoder"
)

// Supported button names conforming to INTENT.md schema specifications.
const (
	ButtonOK    = "ok"
	ButtonBack  = "back"
	ButtonStart = "start"
)

// Supported button actions conforming to INTENT.md schema specifications.
const (
	ActionPress   = "press"
	ActionRelease = "release"
)

// Supported switch positions conforming to INTENT.md schema specifications.
const (
	PositionBusy     = "busy"
	PositionCustom   = "custom"
	PositionOff      = "off"
	PositionApps     = "apps"
	PositionSettings = "settings"
)

// Event represents a normalized in-memory event ready for forwarding to Home Assistant.
type Event struct {
	ID        string                 `json:"-"`
	Device    string                 `json:"device"`
	Type      EventType              `json:"type"`
	Timestamp int64                  `json:"timestamp"`
	CreatedAt time.Time              `json:"-"`
	Data      map[string]interface{} `json:"-"`
	TraceID   int64                  `json:"-"`
}

// ButtonPayload represents the Home Assistant JSON payload for button events per INTENT.md.
type ButtonPayload struct {
	Device    string    `json:"device"`
	Type      EventType `json:"type"`
	Button    string    `json:"button"`
	Action    string    `json:"action"`
	Timestamp int64     `json:"timestamp"`
}

// ToEvent converts a ButtonPayload into a normalized Event.
func (p ButtonPayload) ToEvent() Event {
	return Event{
		Device:    p.Device,
		Type:      EventTypeButton,
		Timestamp: p.Timestamp,
		Data: map[string]interface{}{
			"button": p.Button,
			"action": p.Action,
		},
	}
}

// SwitchPayload represents the Home Assistant JSON payload for switch events per INTENT.md.
type SwitchPayload struct {
	Device    string    `json:"device"`
	Type      EventType `json:"type"`
	Position  string    `json:"position"`
	Timestamp int64     `json:"timestamp"`
}

// ToEvent converts a SwitchPayload into a normalized Event.
func (p SwitchPayload) ToEvent() Event {
	return Event{
		Device:    p.Device,
		Type:      EventTypeSwitch,
		Timestamp: p.Timestamp,
		Data: map[string]interface{}{
			"position": p.Position,
		},
	}
}

// EncoderPayload represents the Home Assistant JSON payload for rotary encoder events per INTENT.md.
type EncoderPayload struct {
	Device    string    `json:"device"`
	Type      EventType `json:"type"`
	Delta     int       `json:"delta"`
	Timestamp int64     `json:"timestamp"`
}

// ToEvent converts an EncoderPayload into a normalized Event.
func (p EncoderPayload) ToEvent() Event {
	return Event{
		Device:    p.Device,
		Type:      EventTypeEncoder,
		Timestamp: p.Timestamp,
		Data: map[string]interface{}{
			"delta": p.Delta,
		},
	}
}

// NewButtonEvent creates an Event configured with button payload fields.
func NewButtonEvent(device, button, action string, ts int64) Event {
	return Event{
		Device:    device,
		Type:      EventTypeButton,
		Timestamp: ts,
		CreatedAt: time.Now(),
		Data: map[string]interface{}{
			"button": button,
			"action": action,
		},
	}
}

// NewSwitchEvent creates an Event configured with switch payload fields.
func NewSwitchEvent(device, position string, ts int64) Event {
	return Event{
		Device:    device,
		Type:      EventTypeSwitch,
		Timestamp: ts,
		CreatedAt: time.Now(),
		Data: map[string]interface{}{
			"position": position,
		},
	}
}

// NewEncoderEvent creates an Event configured with encoder payload fields.
func NewEncoderEvent(device string, delta int, ts int64) Event {
	return Event{
		Device:    device,
		Type:      EventTypeEncoder,
		Timestamp: ts,
		CreatedAt: time.Now(),
		Data: map[string]interface{}{
			"delta": delta,
		},
	}
}

// MarshalJSON serializes the Event into a flat JSON payload matching Home Assistant specifications.
// Arbitrary key-value pairs stored in Data are flattened into the root JSON object alongside device, type, and timestamp.
func (e Event) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{}, len(e.Data)+3)
	for k, v := range e.Data {
		out[k] = v
	}
	out["device"] = e.Device
	out["type"] = e.Type
	out["timestamp"] = e.Timestamp

	return json.Marshal(out)
}

// UnmarshalJSON deserializes a JSON payload into the normalized Event structure, mapping standard fields
// and placing any remaining fields into the Data map.
func (e *Event) UnmarshalJSON(b []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}

	if v, ok := raw["device"].(string); ok {
		e.Device = v
		delete(raw, "device")
	}
	if v, ok := raw["type"].(string); ok {
		e.Type = EventType(v)
		delete(raw, "type")
	}
	if v, ok := raw["timestamp"]; ok {
		switch ts := v.(type) {
		case float64:
			e.Timestamp = int64(ts)
		case int64:
			e.Timestamp = ts
		case json.Number:
			if n, err := ts.Int64(); err == nil {
				e.Timestamp = n
			}
		}
		delete(raw, "timestamp")
	}

	if len(raw) > 0 {
		e.Data = raw
	} else {
		e.Data = nil
	}

	return nil
}

// ToButtonPayload converts the Event into a typed ButtonPayload or returns an error if fields are invalid.
func (e Event) ToButtonPayload() (*ButtonPayload, error) {
	if e.Type != EventTypeButton {
		return nil, fmt.Errorf("invalid event type %q for ButtonPayload", e.Type)
	}
	btn, _ := e.Data["button"].(string)
	if btn == "" {
		return nil, fmt.Errorf("missing or invalid button field in event data")
	}
	action, _ := e.Data["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("missing or invalid action field in event data")
	}
	return &ButtonPayload{
		Device:    e.Device,
		Type:      e.Type,
		Button:    btn,
		Action:    action,
		Timestamp: e.Timestamp,
	}, nil
}

// ToSwitchPayload converts the Event into a typed SwitchPayload or returns an error if fields are invalid.
func (e Event) ToSwitchPayload() (*SwitchPayload, error) {
	if e.Type != EventTypeSwitch {
		return nil, fmt.Errorf("invalid event type %q for SwitchPayload", e.Type)
	}
	pos, _ := e.Data["position"].(string)
	if pos == "" {
		return nil, fmt.Errorf("missing or invalid position field in event data")
	}
	return &SwitchPayload{
		Device:    e.Device,
		Type:      e.Type,
		Position:  pos,
		Timestamp: e.Timestamp,
	}, nil
}

// ToEncoderPayload converts the Event into a typed EncoderPayload or returns an error if fields are invalid.
func (e Event) ToEncoderPayload() (*EncoderPayload, error) {
	if e.Type != EventTypeEncoder {
		return nil, fmt.Errorf("invalid event type %q for EncoderPayload", e.Type)
	}
	rawDelta, ok := e.Data["delta"]
	if !ok {
		return nil, fmt.Errorf("missing delta field in event data")
	}

	var delta int
	switch v := rawDelta.(type) {
	case int:
		delta = v
	case int32:
		delta = int(v)
	case int64:
		delta = int(v)
	case float64:
		delta = int(v)
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return nil, fmt.Errorf("invalid delta value: %w", err)
		}
		delta = int(n)
	default:
		return nil, fmt.Errorf("unsupported type for delta: %T", rawDelta)
	}

	return &EncoderPayload{
		Device:    e.Device,
		Type:      e.Type,
		Delta:     delta,
		Timestamp: e.Timestamp,
	}, nil
}

// ToPayload converts the Event into a concrete payload struct (*ButtonPayload, *SwitchPayload, *EncoderPayload)
// or returns a flattened map[string]interface{} for arbitrary or extensible event types.
func (e Event) ToPayload() (interface{}, error) {
	switch e.Type {
	case EventTypeButton:
		return e.ToButtonPayload()
	case EventTypeSwitch:
		return e.ToSwitchPayload()
	case EventTypeEncoder:
		return e.ToEncoderPayload()
	default:
		out := make(map[string]interface{}, len(e.Data)+3)
		for k, v := range e.Data {
			out[k] = v
		}
		out["device"] = e.Device
		out["type"] = e.Type
		out["timestamp"] = e.Timestamp
		return out, nil
	}
}
