package busybar

import (
	"context"
	"fmt"
	"time"

	"github.com/brotherlogic/busybar-bridge/pkg/pb"
	pb_busybar "github.com/brotherlogic/busybar-bridge/pkg/pb/busybar"
	"google.golang.org/protobuf/proto"
)

// Decoder provides stateless deserialization of upstream binary BSB_State.State frames
// into normalized event representations.
type Decoder struct {
	deviceID string
	recorder MetricsRecorder
}

// NewDecoder constructs a new Decoder instance for the specified device ID and metrics recorder.
// If recorder is nil, a default NoopMetricsRecorder is used.
func NewDecoder(deviceID string, recorder MetricsRecorder) *Decoder {
	if recorder == nil {
		recorder = &NoopMetricsRecorder{}
	}
	return &Decoder{
		deviceID: deviceID,
		recorder: recorder,
	}
}

// Decode deserializes a raw BSB_State.State binary protobuf payload and normalizes all contained
// interaction events (buttons, switches, encoders) into discrete *pb.NormalizedEvent messages.
func (d *Decoder) Decode(payload []byte) ([]*pb.NormalizedEvent, error) {
	var state pb_busybar.State
	if err := proto.Unmarshal(payload, &state); err != nil {
		d.recorder.RecordDecodeError()
		return nil, fmt.Errorf("failed to unmarshal BSB_State.State payload: %w", err)
	}

	ts := int64(state.GetTimestamp())
	if ts <= 0 {
		ts = time.Now().Unix()
	}

	events := make([]*pb.NormalizedEvent, 0)

	for _, update := range state.GetUpdates() {
		if update == nil {
			continue
		}

		input := update.GetInput()
		if input == nil {
			continue
		}

		switch evt := input.GetEvent().(type) {
		case *pb_busybar.InputEvent_ButtonEvent:
			if evt.ButtonEvent == nil {
				continue
			}
			normButton := &pb.NormalizedButtonEvent{
				Device:    d.deviceID,
				Button:    mapButton(evt.ButtonEvent.GetButton()),
				Action:    mapButtonAction(evt.ButtonEvent.GetAction()),
				Timestamp: ts,
			}
			events = append(events, &pb.NormalizedEvent{
				Device:    d.deviceID,
				Timestamp: ts,
				Event: &pb.NormalizedEvent_Button{
					Button: normButton,
				},
			})

		case *pb_busybar.InputEvent_SwitchEvent:
			if evt.SwitchEvent == nil {
				continue
			}
			normSwitch := &pb.NormalizedSwitchEvent{
				Device:    d.deviceID,
				Position:  mapSwitchPosition(evt.SwitchEvent.GetPosition()),
				Timestamp: ts,
			}
			events = append(events, &pb.NormalizedEvent{
				Device:    d.deviceID,
				Timestamp: ts,
				Event: &pb.NormalizedEvent_Switch{
					Switch: normSwitch,
				},
			})

		case *pb_busybar.InputEvent_EncoderEvent:
			if evt.EncoderEvent == nil {
				continue
			}
			normEncoder := &pb.NormalizedEncoderEvent{
				Device:    d.deviceID,
				Delta:     evt.EncoderEvent.GetDelta(),
				Timestamp: ts,
			}
			events = append(events, &pb.NormalizedEvent{
				Device:    d.deviceID,
				Timestamp: ts,
				Event: &pb.NormalizedEvent_Encoder{
					Encoder: normEncoder,
				},
			})

		default:
			// Gracefully ignore unknown input events
			continue
		}
	}

	if len(events) > 0 {
		d.recorder.RecordDecodeSuccess(len(events))
	}

	return events, nil
}

// Run continuously ingests binary frames from the frames channel, decodes them into normalized events,
// and emits them sequentially to the out channel. Saturated out channels are handled non-blockingly by
// dropping the event and invoking RecordDroppedFrame. Cleanly shuts down on context cancellation or channel close.
func (d *Decoder) Run(ctx context.Context, frames <-chan []byte, out chan<- *pb.NormalizedEvent) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-frames:
			if !ok {
				return nil
			}
			events, err := d.Decode(frame)
			if err != nil {
				continue
			}
			for _, evt := range events {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case out <- evt:
				default:
					d.recorder.RecordDroppedFrame()
				}
			}
		}
	}
}

// ToHomeAssistantJSON serializes a NormalizedEvent into Home Assistant JSON payload matching INTENT.md.
func (d *Decoder) ToHomeAssistantJSON(evt *pb.NormalizedEvent) ([]byte, error) {
	return ToHomeAssistantJSON(evt)
}

// ToHomeAssistantJSON serializes a NormalizedEvent into Home Assistant JSON payload matching INTENT.md.
func ToHomeAssistantJSON(evt *pb.NormalizedEvent) ([]byte, error) {
	if evt == nil {
		return nil, fmt.Errorf("normalized event is nil")
	}
	return evt.ToHomeAssistantJSON()
}

func mapButton(b pb_busybar.Button) pb.Button {
	switch b {
	case pb_busybar.Button_OK:
		return pb.Button_BUTTON_OK
	case pb_busybar.Button_BACK:
		return pb.Button_BUTTON_BACK
	case pb_busybar.Button_START:
		return pb.Button_BUTTON_START
	default:
		return pb.Button_BUTTON_UNKNOWN
	}
}

func mapButtonAction(a pb_busybar.ButtonAction) pb.ButtonAction {
	switch a {
	case pb_busybar.ButtonAction_PRESS:
		return pb.ButtonAction_ACTION_PRESS
	case pb_busybar.ButtonAction_RELEASE:
		return pb.ButtonAction_ACTION_RELEASE
	default:
		return pb.ButtonAction_ACTION_UNKNOWN
	}
}

func mapSwitchPosition(p pb_busybar.SwitchPosition) pb.SwitchPosition {
	switch p {
	case pb_busybar.SwitchPosition_BUSY:
		return pb.SwitchPosition_SWITCH_BUSY
	case pb_busybar.SwitchPosition_CUSTOM:
		return pb.SwitchPosition_SWITCH_CUSTOM
	case pb_busybar.SwitchPosition_OFF:
		return pb.SwitchPosition_SWITCH_OFF
	case pb_busybar.SwitchPosition_APPS:
		return pb.SwitchPosition_SWITCH_APPS
	case pb_busybar.SwitchPosition_SETTINGS:
		return pb.SwitchPosition_SWITCH_SETTINGS
	default:
		return pb.SwitchPosition_SWITCH_UNKNOWN
	}
}
