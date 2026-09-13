package hass_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/hass"
)

func TestEventMarshaling_Button(t *testing.T) {
	event := hass.NewButtonEvent("busybar", hass.ButtonOK, hass.ActionPress, 1726180000)
	event.ID = "evt-123"
	event.CreatedAt = time.Now()

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	expected := map[string]interface{}{
		"device":    "busybar",
		"type":      "button",
		"button":    "ok",
		"action":    "press",
		"timestamp": float64(1726180000),
	}

	if !reflect.DeepEqual(raw, expected) {
		t.Errorf("marshaled button JSON mismatch.\nGot:  %#v\nWant: %#v", raw, expected)
	}

	// Verify ID and CreatedAt are omitted in JSON
	if _, exists := raw["id"]; exists {
		t.Errorf("expected 'id' to be omitted in JSON")
	}
	if _, exists := raw["created_at"]; exists {
		t.Errorf("expected 'created_at' to be omitted in JSON")
	}
	if _, exists := raw["data"]; exists {
		t.Errorf("expected 'data' wrapper to be omitted in JSON")
	}

	// Test ToButtonPayload helper
	payload, err := event.ToButtonPayload()
	if err != nil {
		t.Fatalf("ToButtonPayload failed: %v", err)
	}
	if payload.Device != "busybar" || payload.Type != hass.EventTypeButton || payload.Button != "ok" || payload.Action != "press" || payload.Timestamp != 1726180000 {
		t.Errorf("ToButtonPayload result mismatch: %+v", payload)
	}

	// Test payload direct JSON marshaling matches INTENT.md
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) failed: %v", err)
	}
	var payloadRaw map[string]interface{}
	if err := json.Unmarshal(payloadJSON, &payloadRaw); err != nil {
		t.Fatalf("json.Unmarshal(payloadJSON) failed: %v", err)
	}
	if !reflect.DeepEqual(payloadRaw, expected) {
		t.Errorf("ButtonPayload JSON mismatch.\nGot:  %#v\nWant: %#v", payloadRaw, expected)
	}
}

func TestEventMarshaling_Switch(t *testing.T) {
	event := hass.NewSwitchEvent("busybar", hass.PositionBusy, 1726180001)
	event.ID = "evt-456"
	event.CreatedAt = time.Now()

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	expected := map[string]interface{}{
		"device":    "busybar",
		"type":      "switch",
		"position":  "busy",
		"timestamp": float64(1726180001),
	}

	if !reflect.DeepEqual(raw, expected) {
		t.Errorf("marshaled switch JSON mismatch.\nGot:  %#v\nWant: %#v", raw, expected)
	}

	// Test ToSwitchPayload helper
	payload, err := event.ToSwitchPayload()
	if err != nil {
		t.Fatalf("ToSwitchPayload failed: %v", err)
	}
	if payload.Device != "busybar" || payload.Type != hass.EventTypeSwitch || payload.Position != "busy" || payload.Timestamp != 1726180001 {
		t.Errorf("ToSwitchPayload result mismatch: %+v", payload)
	}

	// Test SwitchPayload direct JSON marshaling matches INTENT.md
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) failed: %v", err)
	}
	var payloadRaw map[string]interface{}
	if err := json.Unmarshal(payloadJSON, &payloadRaw); err != nil {
		t.Fatalf("json.Unmarshal(payloadJSON) failed: %v", err)
	}
	if !reflect.DeepEqual(payloadRaw, expected) {
		t.Errorf("SwitchPayload JSON mismatch.\nGot:  %#v\nWant: %#v", payloadRaw, expected)
	}
}

func TestEventMarshaling_Encoder(t *testing.T) {
	event := hass.NewEncoderEvent("busybar", 1, 1726180002)
	event.ID = "evt-789"
	event.CreatedAt = time.Now()

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	expected := map[string]interface{}{
		"device":    "busybar",
		"type":      "encoder",
		"delta":     float64(1),
		"timestamp": float64(1726180002),
	}

	if !reflect.DeepEqual(raw, expected) {
		t.Errorf("marshaled encoder JSON mismatch.\nGot:  %#v\nWant: %#v", raw, expected)
	}

	// Test ToEncoderPayload helper
	payload, err := event.ToEncoderPayload()
	if err != nil {
		t.Fatalf("ToEncoderPayload failed: %v", err)
	}
	if payload.Device != "busybar" || payload.Type != hass.EventTypeEncoder || payload.Delta != 1 || payload.Timestamp != 1726180002 {
		t.Errorf("ToEncoderPayload result mismatch: %+v", payload)
	}

	// Test EncoderPayload direct JSON marshaling matches INTENT.md
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) failed: %v", err)
	}
	var payloadRaw map[string]interface{}
	if err := json.Unmarshal(payloadJSON, &payloadRaw); err != nil {
		t.Fatalf("json.Unmarshal(payloadJSON) failed: %v", err)
	}
	if !reflect.DeepEqual(payloadRaw, expected) {
		t.Errorf("EncoderPayload JSON mismatch.\nGot:  %#v\nWant: %#v", payloadRaw, expected)
	}
}

func TestEventMarshaling_ArbitraryData(t *testing.T) {
	event := hass.Event{
		ID:        "evt-custom-99",
		Device:    "busybar",
		Type:      hass.EventType("battery"),
		Timestamp: 1726180010,
		CreatedAt: time.Now(),
		Data: map[string]interface{}{
			"level":    85,
			"charging": true,
			"source":   "usb-c",
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	expected := map[string]interface{}{
		"device":    "busybar",
		"type":      "battery",
		"timestamp": float64(1726180010),
		"level":     float64(85),
		"charging":  true,
		"source":    "usb-c",
	}

	if !reflect.DeepEqual(raw, expected) {
		t.Errorf("marshaled arbitrary event JSON mismatch.\nGot:  %#v\nWant: %#v", raw, expected)
	}

	// Test ToPayload helper fallback for arbitrary types
	payload, err := event.ToPayload()
	if err != nil {
		t.Fatalf("ToPayload failed: %v", err)
	}
	m, ok := payload.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{} from ToPayload for arbitrary type, got %T", payload)
	}
	if m["level"] != 85 || m["charging"] != true || m["source"] != "usb-c" {
		t.Errorf("unexpected arbitrary payload contents: %#v", m)
	}
}

func TestEventUnmarshaling(t *testing.T) {
	inputJSON := `{"device":"busybar","type":"button","button":"back","action":"release","timestamp":1726180020}`

	var event hass.Event
	if err := json.Unmarshal([]byte(inputJSON), &event); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if event.Device != "busybar" {
		t.Errorf("expected Device 'busybar', got %q", event.Device)
	}
	if event.Type != hass.EventTypeButton {
		t.Errorf("expected Type 'button', got %q", event.Type)
	}
	if event.Timestamp != 1726180020 {
		t.Errorf("expected Timestamp 1726180020, got %d", event.Timestamp)
	}
	if event.Data["button"] != "back" {
		t.Errorf("expected button 'back', got %v", event.Data["button"])
	}
	if event.Data["action"] != "release" {
		t.Errorf("expected action 'release', got %v", event.Data["action"])
	}

	btnPayload, err := event.ToButtonPayload()
	if err != nil {
		t.Fatalf("ToButtonPayload failed on unmarshaled event: %v", err)
	}
	if btnPayload.Button != "back" || btnPayload.Action != "release" {
		t.Errorf("unexpected ButtonPayload: %+v", btnPayload)
	}
}

func TestPayloadHelpers_InvalidType(t *testing.T) {
	btnEvent := hass.NewButtonEvent("busybar", hass.ButtonStart, hass.ActionPress, 1726180030)

	if _, err := btnEvent.ToSwitchPayload(); err == nil {
		t.Errorf("expected error converting button event to SwitchPayload, got nil")
	}
	if _, err := btnEvent.ToEncoderPayload(); err == nil {
		t.Errorf("expected error converting button event to EncoderPayload, got nil")
	}

	switchEvent := hass.NewSwitchEvent("busybar", hass.PositionOff, 1726180031)
	if _, err := switchEvent.ToButtonPayload(); err == nil {
		t.Errorf("expected error converting switch event to ButtonPayload, got nil")
	}
	if _, err := switchEvent.ToEncoderPayload(); err == nil {
		t.Errorf("expected error converting switch event to EncoderPayload, got nil")
	}

	encoderEvent := hass.NewEncoderEvent("busybar", -1, 1726180032)
	if _, err := encoderEvent.ToButtonPayload(); err == nil {
		t.Errorf("expected error converting encoder event to ButtonPayload, got nil")
	}
	if _, err := encoderEvent.ToSwitchPayload(); err == nil {
		t.Errorf("expected error converting encoder event to SwitchPayload, got nil")
	}
}

func TestPayload_ToEvent(t *testing.T) {
	bp := hass.ButtonPayload{
		Device:    "busybar",
		Type:      hass.EventTypeButton,
		Button:    hass.ButtonOK,
		Action:    hass.ActionPress,
		Timestamp: 100,
	}
	ev := bp.ToEvent()
	if ev.Device != bp.Device || ev.Type != bp.Type || ev.Timestamp != bp.Timestamp {
		t.Errorf("ButtonPayload.ToEvent() mismatch: %+v", ev)
	}
	if ev.Data["button"] != hass.ButtonOK || ev.Data["action"] != hass.ActionPress {
		t.Errorf("ButtonPayload.ToEvent() Data mismatch: %#v", ev.Data)
	}

	sp := hass.SwitchPayload{
		Device:    "busybar",
		Type:      hass.EventTypeSwitch,
		Position:  hass.PositionCustom,
		Timestamp: 200,
	}
	ev2 := sp.ToEvent()
	if ev2.Device != sp.Device || ev2.Type != sp.Type || ev2.Timestamp != sp.Timestamp {
		t.Errorf("SwitchPayload.ToEvent() mismatch: %+v", ev2)
	}
	if ev2.Data["position"] != hass.PositionCustom {
		t.Errorf("SwitchPayload.ToEvent() Data mismatch: %#v", ev2.Data)
	}

	ep := hass.EncoderPayload{
		Device:    "busybar",
		Type:      hass.EventTypeEncoder,
		Delta:     -2,
		Timestamp: 300,
	}
	ev3 := ep.ToEvent()
	if ev3.Device != ep.Device || ev3.Type != ep.Type || ev3.Timestamp != ep.Timestamp {
		t.Errorf("EncoderPayload.ToEvent() mismatch: %+v", ev3)
	}
	if ev3.Data["delta"] != -2 {
		t.Errorf("EncoderPayload.ToEvent() Data mismatch: %#v", ev3.Data)
	}
}
