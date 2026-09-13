package pb_test

import (
	"testing"

	"github.com/brotherlogic/busybar-bridge/pkg/pb"
	"google.golang.org/protobuf/proto"
)

func TestNormalizedEventSerialization(t *testing.T) {
	tests := []struct {
		name  string
		event *pb.NormalizedEvent
	}{
		{
			name: "ButtonEvent",
			event: &pb.NormalizedEvent{
				Device:    "device-1",
				Timestamp: 1710000000,
				Event: &pb.NormalizedEvent_Button{
					Button: &pb.NormalizedButtonEvent{
						Device:    "device-1",
						Button:    pb.Button_BUTTON_OK,
						Action:    pb.ButtonAction_ACTION_PRESS,
						Timestamp: 1710000000,
					},
				},
			},
		},
		{
			name: "SwitchEvent",
			event: &pb.NormalizedEvent{
				Device:    "device-1",
				Timestamp: 1710000001,
				Event: &pb.NormalizedEvent_Switch{
					Switch: &pb.NormalizedSwitchEvent{
						Device:    "device-1",
						Position:  pb.SwitchPosition_SWITCH_BUSY,
						Timestamp: 1710000001,
					},
				},
			},
		},
		{
			name: "EncoderEvent",
			event: &pb.NormalizedEvent{
				Device:    "device-1",
				Timestamp: 1710000002,
				Event: &pb.NormalizedEvent_Encoder{
					Encoder: &pb.NormalizedEncoderEvent{
						Device:    "device-1",
						Delta:     -3,
						Timestamp: 1710000002,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := proto.Marshal(tt.event)
			if err != nil {
				t.Fatalf("proto.Marshal failed: %v", err)
			}

			unmarshaled := &pb.NormalizedEvent{}
			if err := proto.Unmarshal(data, unmarshaled); err != nil {
				t.Fatalf("proto.Unmarshal failed: %v", err)
			}

			if !proto.Equal(tt.event, unmarshaled) {
				t.Errorf("unmarshaled event mismatch: got %v, want %v", unmarshaled, tt.event)
			}
		})
	}
}
