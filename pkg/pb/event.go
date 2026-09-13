package pb

import (
	"encoding/json"
	"fmt"
)

// HomeAssistantButtonPayload represents the JSON payload for a button event matching INTENT.md.
type HomeAssistantButtonPayload struct {
	Device    string `json:"device"`
	Type      string `json:"type"`
	Button    string `json:"button"`
	Action    string `json:"action"`
	Timestamp int64  `json:"timestamp"`
}

// HomeAssistantSwitchPayload represents the JSON payload for a switch event matching INTENT.md.
type HomeAssistantSwitchPayload struct {
	Device    string `json:"device"`
	Type      string `json:"type"`
	Position  string `json:"position"`
	Timestamp int64  `json:"timestamp"`
}

// HomeAssistantEncoderPayload represents the JSON payload for an encoder event matching INTENT.md.
type HomeAssistantEncoderPayload struct {
	Device    string `json:"device"`
	Type      string `json:"type"`
	Delta     int    `json:"delta"`
	Timestamp int64  `json:"timestamp"`
}

// ToHomeAssistantJSON serializes a NormalizedEvent into Home Assistant JSON payload matching INTENT.md.
func (e *NormalizedEvent) ToHomeAssistantJSON() ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("normalized event is nil")
	}

	device := e.GetDevice()
	ts := e.GetTimestamp()

	switch evt := e.GetEvent().(type) {
	case *NormalizedEvent_Button:
		if evt.Button == nil {
			return nil, fmt.Errorf("missing button details in button event")
		}
		if device == "" {
			device = evt.Button.GetDevice()
		}
		if ts == 0 {
			ts = evt.Button.GetTimestamp()
		}

		var btnStr string
		switch evt.Button.GetButton() {
		case Button_BUTTON_OK:
			btnStr = "ok"
		case Button_BUTTON_BACK:
			btnStr = "back"
		case Button_BUTTON_START:
			btnStr = "start"
		default:
			btnStr = "unknown"
		}

		var actionStr string
		switch evt.Button.GetAction() {
		case ButtonAction_ACTION_PRESS:
			actionStr = "press"
		case ButtonAction_ACTION_RELEASE:
			actionStr = "release"
		default:
			actionStr = "unknown"
		}

		payload := HomeAssistantButtonPayload{
			Device:    device,
			Type:      "button",
			Button:    btnStr,
			Action:    actionStr,
			Timestamp: ts,
		}
		return json.Marshal(payload)

	case *NormalizedEvent_Switch:
		if evt.Switch == nil {
			return nil, fmt.Errorf("missing switch details in switch event")
		}
		if device == "" {
			device = evt.Switch.GetDevice()
		}
		if ts == 0 {
			ts = evt.Switch.GetTimestamp()
		}

		var posStr string
		switch evt.Switch.GetPosition() {
		case SwitchPosition_SWITCH_BUSY:
			posStr = "busy"
		case SwitchPosition_SWITCH_CUSTOM:
			posStr = "custom"
		case SwitchPosition_SWITCH_OFF:
			posStr = "off"
		case SwitchPosition_SWITCH_APPS:
			posStr = "apps"
		case SwitchPosition_SWITCH_SETTINGS:
			posStr = "settings"
		default:
			posStr = "unknown"
		}

		payload := HomeAssistantSwitchPayload{
			Device:    device,
			Type:      "switch",
			Position:  posStr,
			Timestamp: ts,
		}
		return json.Marshal(payload)

	case *NormalizedEvent_Encoder:
		if evt.Encoder == nil {
			return nil, fmt.Errorf("missing encoder details in encoder event")
		}
		if device == "" {
			device = evt.Encoder.GetDevice()
		}
		if ts == 0 {
			ts = evt.Encoder.GetTimestamp()
		}

		payload := HomeAssistantEncoderPayload{
			Device:    device,
			Type:      "encoder",
			Delta:     int(evt.Encoder.GetDelta()),
			Timestamp: ts,
		}
		return json.Marshal(payload)

	default:
		return nil, fmt.Errorf("unsupported or nil event type: %T", e.GetEvent())
	}
}
