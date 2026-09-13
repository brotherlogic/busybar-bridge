package busybar_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/busybar"
	"github.com/brotherlogic/busybar-bridge/pkg/pb"
	pb_busybar "github.com/brotherlogic/busybar-bridge/pkg/pb/busybar"
	"google.golang.org/protobuf/proto"
)

type mockMetricsRecorder struct {
	mu                  sync.Mutex
	decodeSuccessCalls  int
	decodeSuccessEvents int
	decodeErrorCalls    int
	droppedFrameCalls   int
}

func (m *mockMetricsRecorder) RecordDecodeSuccess(eventCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decodeSuccessCalls++
	m.decodeSuccessEvents += eventCount
}

func (m *mockMetricsRecorder) RecordDecodeError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decodeErrorCalls++
}

func (m *mockMetricsRecorder) RecordDroppedFrame() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.droppedFrameCalls++
}

func (m *mockMetricsRecorder) stats() (successCalls, successEvents, errorCalls, dropCalls int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.decodeSuccessCalls, m.decodeSuccessEvents, m.decodeErrorCalls, m.droppedFrameCalls
}

func TestNoopMetricsRecorder(t *testing.T) {
	var r busybar.MetricsRecorder = &busybar.NoopMetricsRecorder{}
	r.RecordDecodeSuccess(5)
	r.RecordDecodeError()
	r.RecordDroppedFrame()

	var val busybar.MetricsRecorder = busybar.NoopMetricsRecorder{}
	val.RecordDecodeSuccess(2)
	val.RecordDecodeError()
	val.RecordDroppedFrame()
}

func TestNilMetricsRecorderFallback(t *testing.T) {
	d := busybar.NewDecoder("device-test", nil)
	if d == nil {
		t.Fatal("expected non-nil Decoder")
	}

	state := &pb_busybar.State{
		Timestamp: 1726180000,
		Updates: []*pb_busybar.StateUpdate{
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_ButtonEvent{
							ButtonEvent: &pb_busybar.ButtonEvent{
								Button: pb_busybar.Button_OK,
								Action: pb_busybar.ButtonAction_PRESS,
							},
						},
					},
				},
			},
		},
	}
	payload, err := proto.Marshal(state)
	if err != nil {
		t.Fatalf("failed to marshal state: %v", err)
	}

	events, err := d.Decode(payload)
	if err != nil {
		t.Fatalf("expected nil error with nil metrics recorder, got: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Also verify decoding error with nil recorder doesn't panic
	_, err = d.Decode([]byte{0xff, 0xff, 0xff})
	if err == nil {
		t.Fatal("expected unmarshal error on corrupt data")
	}
}

func TestDecodeButtons(t *testing.T) {
	tests := []struct {
		name           string
		inputButton    pb_busybar.Button
		inputAction    pb_busybar.ButtonAction
		expectedButton pb.Button
		expectedAction pb.ButtonAction
	}{
		{
			name:           "Button OK PRESS",
			inputButton:    pb_busybar.Button_OK,
			inputAction:    pb_busybar.ButtonAction_PRESS,
			expectedButton: pb.Button_BUTTON_OK,
			expectedAction: pb.ButtonAction_ACTION_PRESS,
		},
		{
			name:           "Button OK RELEASE",
			inputButton:    pb_busybar.Button_OK,
			inputAction:    pb_busybar.ButtonAction_RELEASE,
			expectedButton: pb.Button_BUTTON_OK,
			expectedAction: pb.ButtonAction_ACTION_RELEASE,
		},
		{
			name:           "Button BACK PRESS",
			inputButton:    pb_busybar.Button_BACK,
			inputAction:    pb_busybar.ButtonAction_PRESS,
			expectedButton: pb.Button_BUTTON_BACK,
			expectedAction: pb.ButtonAction_ACTION_PRESS,
		},
		{
			name:           "Button BACK RELEASE",
			inputButton:    pb_busybar.Button_BACK,
			inputAction:    pb_busybar.ButtonAction_RELEASE,
			expectedButton: pb.Button_BUTTON_BACK,
			expectedAction: pb.ButtonAction_ACTION_RELEASE,
		},
		{
			name:           "Button START PRESS",
			inputButton:    pb_busybar.Button_START,
			inputAction:    pb_busybar.ButtonAction_PRESS,
			expectedButton: pb.Button_BUTTON_START,
			expectedAction: pb.ButtonAction_ACTION_PRESS,
		},
		{
			name:           "Button START RELEASE",
			inputButton:    pb_busybar.Button_START,
			inputAction:    pb_busybar.ButtonAction_RELEASE,
			expectedButton: pb.Button_BUTTON_START,
			expectedAction: pb.ButtonAction_ACTION_RELEASE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &mockMetricsRecorder{}
			d := busybar.NewDecoder("test-device-btn", recorder)

			state := &pb_busybar.State{
				Timestamp: 1726181000,
				Updates: []*pb_busybar.StateUpdate{
					{
						State: &pb_busybar.StateUpdate_Input{
							Input: &pb_busybar.InputEvent{
								Event: &pb_busybar.InputEvent_ButtonEvent{
									ButtonEvent: &pb_busybar.ButtonEvent{
										Button: tt.inputButton,
										Action: tt.inputAction,
									},
								},
							},
						},
					},
				},
			}
			payload, err := proto.Marshal(state)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}

			events, err := d.Decode(payload)
			if err != nil {
				t.Fatalf("unexpected decode error: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("expected 1 event, got %d", len(events))
			}

			ev := events[0]
			if ev.GetDevice() != "test-device-btn" {
				t.Errorf("expected device 'test-device-btn', got '%s'", ev.GetDevice())
			}
			if ev.GetTimestamp() != 1726181000 {
				t.Errorf("expected timestamp 1726181000, got %d", ev.GetTimestamp())
			}

			btn := ev.GetButton()
			if btn == nil {
				t.Fatal("expected Button event to be non-nil")
			}
			if btn.GetDevice() != "test-device-btn" {
				t.Errorf("expected btn device 'test-device-btn', got '%s'", btn.GetDevice())
			}
			if btn.GetButton() != tt.expectedButton {
				t.Errorf("expected button %v, got %v", tt.expectedButton, btn.GetButton())
			}
			if btn.GetAction() != tt.expectedAction {
				t.Errorf("expected action %v, got %v", tt.expectedAction, btn.GetAction())
			}
			if btn.GetTimestamp() != 1726181000 {
				t.Errorf("expected btn timestamp 1726181000, got %d", btn.GetTimestamp())
			}

			successCalls, successEvents, errorCalls, dropCalls := recorder.stats()
			if successCalls != 1 || successEvents != 1 {
				t.Errorf("expected 1 success call with 1 event, got calls=%d, events=%d", successCalls, successEvents)
			}
			if errorCalls != 0 || dropCalls != 0 {
				t.Errorf("expected 0 error/drop calls, got errorCalls=%d, dropCalls=%d", errorCalls, dropCalls)
			}
		})
	}
}

func TestDecodeSwitches(t *testing.T) {
	tests := []struct {
		name             string
		inputPosition    pb_busybar.SwitchPosition
		expectedPosition pb.SwitchPosition
	}{
		{
			name:             "Switch BUSY",
			inputPosition:    pb_busybar.SwitchPosition_BUSY,
			expectedPosition: pb.SwitchPosition_SWITCH_BUSY,
		},
		{
			name:             "Switch CUSTOM",
			inputPosition:    pb_busybar.SwitchPosition_CUSTOM,
			expectedPosition: pb.SwitchPosition_SWITCH_CUSTOM,
		},
		{
			name:             "Switch OFF",
			inputPosition:    pb_busybar.SwitchPosition_OFF,
			expectedPosition: pb.SwitchPosition_SWITCH_OFF,
		},
		{
			name:             "Switch APPS",
			inputPosition:    pb_busybar.SwitchPosition_APPS,
			expectedPosition: pb.SwitchPosition_SWITCH_APPS,
		},
		{
			name:             "Switch SETTINGS",
			inputPosition:    pb_busybar.SwitchPosition_SETTINGS,
			expectedPosition: pb.SwitchPosition_SWITCH_SETTINGS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &mockMetricsRecorder{}
			d := busybar.NewDecoder("test-device-sw", recorder)

			state := &pb_busybar.State{
				Timestamp: 1726182000,
				Updates: []*pb_busybar.StateUpdate{
					{
						State: &pb_busybar.StateUpdate_Input{
							Input: &pb_busybar.InputEvent{
								Event: &pb_busybar.InputEvent_SwitchEvent{
									SwitchEvent: &pb_busybar.SwitchEvent{
										Position: tt.inputPosition,
									},
								},
							},
						},
					},
				},
			}
			payload, err := proto.Marshal(state)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}

			events, err := d.Decode(payload)
			if err != nil {
				t.Fatalf("unexpected decode error: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("expected 1 event, got %d", len(events))
			}

			ev := events[0]
			if ev.GetDevice() != "test-device-sw" {
				t.Errorf("expected device 'test-device-sw', got '%s'", ev.GetDevice())
			}
			if ev.GetTimestamp() != 1726182000 {
				t.Errorf("expected timestamp 1726182000, got %d", ev.GetTimestamp())
			}

			sw := ev.GetSwitch()
			if sw == nil {
				t.Fatal("expected Switch event to be non-nil")
			}
			if sw.GetDevice() != "test-device-sw" {
				t.Errorf("expected sw device 'test-device-sw', got '%s'", sw.GetDevice())
			}
			if sw.GetPosition() != tt.expectedPosition {
				t.Errorf("expected position %v, got %v", tt.expectedPosition, sw.GetPosition())
			}
			if sw.GetTimestamp() != 1726182000 {
				t.Errorf("expected sw timestamp 1726182000, got %d", sw.GetTimestamp())
			}

			successCalls, successEvents, errorCalls, _ := recorder.stats()
			if successCalls != 1 || successEvents != 1 || errorCalls != 0 {
				t.Errorf("metrics mismatch: successCalls=%d, events=%d, errors=%d", successCalls, successEvents, errorCalls)
			}
		})
	}
}

func TestDecodeEncoder(t *testing.T) {
	deltas := []int32{1, -1, 12, -5}

	for _, delta := range deltas {
		t.Run("delta", func(t *testing.T) {
			recorder := &mockMetricsRecorder{}
			d := busybar.NewDecoder("test-device-enc", recorder)

			state := &pb_busybar.State{
				Timestamp: 1726183000,
				Updates: []*pb_busybar.StateUpdate{
					{
						State: &pb_busybar.StateUpdate_Input{
							Input: &pb_busybar.InputEvent{
								Event: &pb_busybar.InputEvent_EncoderEvent{
									EncoderEvent: &pb_busybar.EncoderEvent{
										Delta: delta,
									},
								},
							},
						},
					},
				},
			}
			payload, err := proto.Marshal(state)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}

			events, err := d.Decode(payload)
			if err != nil {
				t.Fatalf("unexpected decode error: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("expected 1 event, got %d", len(events))
			}

			ev := events[0]
			if ev.GetDevice() != "test-device-enc" {
				t.Errorf("expected device 'test-device-enc', got '%s'", ev.GetDevice())
			}
			if ev.GetTimestamp() != 1726183000 {
				t.Errorf("expected timestamp 1726183000, got %d", ev.GetTimestamp())
			}

			enc := ev.GetEncoder()
			if enc == nil {
				t.Fatal("expected Encoder event to be non-nil")
			}
			if enc.GetDevice() != "test-device-enc" {
				t.Errorf("expected enc device 'test-device-enc', got '%s'", enc.GetDevice())
			}
			if enc.GetDelta() != delta {
				t.Errorf("expected delta %d, got %d", delta, enc.GetDelta())
			}
			if enc.GetTimestamp() != 1726183000 {
				t.Errorf("expected enc timestamp 1726183000, got %d", enc.GetTimestamp())
			}

			successCalls, successEvents, errorCalls, _ := recorder.stats()
			if successCalls != 1 || successEvents != 1 || errorCalls != 0 {
				t.Errorf("metrics mismatch: successCalls=%d, events=%d, errors=%d", successCalls, successEvents, errorCalls)
			}
		})
	}
}

func TestMultiUpdateFrameOrdering(t *testing.T) {
	recorder := &mockMetricsRecorder{}
	d := busybar.NewDecoder("test-device-multi", recorder)

	state := &pb_busybar.State{
		Timestamp: 1726184000,
		Updates: []*pb_busybar.StateUpdate{
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_EncoderEvent{
							EncoderEvent: &pb_busybar.EncoderEvent{Delta: 1},
						},
					},
				},
			},
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_ButtonEvent{
							ButtonEvent: &pb_busybar.ButtonEvent{
								Button: pb_busybar.Button_OK,
								Action: pb_busybar.ButtonAction_PRESS,
							},
						},
					},
				},
			},
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_ButtonEvent{
							ButtonEvent: &pb_busybar.ButtonEvent{
								Button: pb_busybar.Button_OK,
								Action: pb_busybar.ButtonAction_RELEASE,
							},
						},
					},
				},
			},
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_SwitchEvent{
							SwitchEvent: &pb_busybar.SwitchEvent{
								Position: pb_busybar.SwitchPosition_BUSY,
							},
						},
					},
				},
			},
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_EncoderEvent{
							EncoderEvent: &pb_busybar.EncoderEvent{Delta: -5},
						},
					},
				},
			},
		},
	}
	payload, err := proto.Marshal(state)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	events, err := d.Decode(payload)
	if err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}

	// 1: Encoder delta +1
	if ev := events[0].GetEncoder(); ev == nil || ev.GetDelta() != 1 {
		t.Errorf("event 0 expected encoder delta +1, got %v", ev)
	}
	// 2: Button OK PRESS
	if ev := events[1].GetButton(); ev == nil || ev.GetButton() != pb.Button_BUTTON_OK || ev.GetAction() != pb.ButtonAction_ACTION_PRESS {
		t.Errorf("event 1 expected button OK PRESS, got %v", ev)
	}
	// 3: Button OK RELEASE
	if ev := events[2].GetButton(); ev == nil || ev.GetButton() != pb.Button_BUTTON_OK || ev.GetAction() != pb.ButtonAction_ACTION_RELEASE {
		t.Errorf("event 2 expected button OK RELEASE, got %v", ev)
	}
	// 4: Switch BUSY
	if ev := events[3].GetSwitch(); ev == nil || ev.GetPosition() != pb.SwitchPosition_SWITCH_BUSY {
		t.Errorf("event 3 expected switch BUSY, got %v", ev)
	}
	// 5: Encoder delta -5
	if ev := events[4].GetEncoder(); ev == nil || ev.GetDelta() != -5 {
		t.Errorf("event 4 expected encoder delta -5, got %v", ev)
	}

	successCalls, successEvents, errorCalls, _ := recorder.stats()
	if successCalls != 1 || successEvents != 5 || errorCalls != 0 {
		t.Errorf("expected 1 call with 5 events, got calls=%d, events=%d, errors=%d", successCalls, successEvents, errorCalls)
	}
}

func TestCorruptedAndTruncatedFrames(t *testing.T) {
	recorder := &mockMetricsRecorder{}
	d := busybar.NewDecoder("test-device-corrupt", recorder)

	// Case 1: Random garbage bytes
	garbage := []byte{0xff, 0xff, 0xff, 0x01, 0x02}
	events, err := d.Decode(garbage)
	if err == nil {
		t.Error("expected error for corrupted garbage payload, got nil")
	}
	if events != nil {
		t.Errorf("expected nil events on error, got %v", events)
	}

	// Case 2: Truncated valid frame
	validState := &pb_busybar.State{
		Timestamp: 1726185000,
		Updates: []*pb_busybar.StateUpdate{
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_EncoderEvent{
							EncoderEvent: &pb_busybar.EncoderEvent{Delta: 42},
						},
					},
				},
			},
		},
	}
	validBytes, err := proto.Marshal(validState)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	truncated := validBytes[:len(validBytes)-1]
	events, err = d.Decode(truncated)
	if err == nil {
		t.Error("expected error for truncated payload, got nil")
	}
	if events != nil {
		t.Errorf("expected nil events on error, got %v", events)
	}

	_, _, errorCalls, _ := recorder.stats()
	if errorCalls != 2 {
		t.Errorf("expected 2 decode error calls, got %d", errorCalls)
	}
}

func TestEmptyAndNonInputFrames(t *testing.T) {
	recorder := &mockMetricsRecorder{}
	d := busybar.NewDecoder("test-device-empty", recorder)

	// 1. Empty payload
	events, err := d.Decode([]byte{})
	if err != nil {
		t.Errorf("expected nil error for empty payload, got: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}

	// 2. Empty State message (no updates)
	emptyState := &pb_busybar.State{Timestamp: 1726186000}
	emptyBytes, _ := proto.Marshal(emptyState)
	events, err = d.Decode(emptyBytes)
	if err != nil {
		t.Errorf("expected nil error for empty state, got: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}

	// 3. State frame containing only non-input updates (WiFi, Brightness, Power)
	nonInputState := &pb_busybar.State{
		Timestamp: 1726186000,
		Updates: []*pb_busybar.StateUpdate{
			{
				State: &pb_busybar.StateUpdate_Wifi{
					Wifi: &pb_busybar.Wifi{},
				},
			},
			{
				State: &pb_busybar.StateUpdate_Brightness{
					Brightness: &pb_busybar.Brightness{},
				},
			},
			{
				State: &pb_busybar.StateUpdate_Power{
					Power: &pb_busybar.Power{},
				},
			},
		},
	}
	nonInputBytes, _ := proto.Marshal(nonInputState)
	events, err = d.Decode(nonInputBytes)
	if err != nil {
		t.Errorf("expected nil error for non-input state, got: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}

	// 4. Mixed frame: non-input updates interleaved with valid input events
	mixedState := &pb_busybar.State{
		Timestamp: 1726186000,
		Updates: []*pb_busybar.StateUpdate{
			{
				State: &pb_busybar.StateUpdate_Wifi{
					Wifi: &pb_busybar.Wifi{},
				},
			},
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_ButtonEvent{
							ButtonEvent: &pb_busybar.ButtonEvent{
								Button: pb_busybar.Button_START,
								Action: pb_busybar.ButtonAction_PRESS,
							},
						},
					},
				},
			},
			{
				State: &pb_busybar.StateUpdate_Brightness{
					Brightness: &pb_busybar.Brightness{},
				},
			},
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_EncoderEvent{
							EncoderEvent: &pb_busybar.EncoderEvent{Delta: 7},
						},
					},
				},
			},
		},
	}
	mixedBytes, _ := proto.Marshal(mixedState)
	events, err = d.Decode(mixedBytes)
	if err != nil {
		t.Fatalf("unexpected error for mixed state: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events extracted, got %d", len(events))
	}
	if btn := events[0].GetButton(); btn == nil || btn.GetButton() != pb.Button_BUTTON_START {
		t.Errorf("first event expected START button, got %v", btn)
	}
	if enc := events[1].GetEncoder(); enc == nil || enc.GetDelta() != 7 {
		t.Errorf("second event expected encoder delta 7, got %v", enc)
	}

	// Verify recorder: only 1 success call (for the mixed frame with 2 events) and 0 errors
	successCalls, successEvents, errorCalls, _ := recorder.stats()
	if successCalls != 1 || successEvents != 2 || errorCalls != 0 {
		t.Errorf("metrics mismatch: successCalls=%d, successEvents=%d, errorCalls=%d", successCalls, successEvents, errorCalls)
	}
}

func TestTimestampFallback(t *testing.T) {
	recorder := &mockMetricsRecorder{}
	d := busybar.NewDecoder("test-device-ts", recorder)

	state := &pb_busybar.State{
		Timestamp: 0, // Zero timestamp should trigger fallback
		Updates: []*pb_busybar.StateUpdate{
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_ButtonEvent{
							ButtonEvent: &pb_busybar.ButtonEvent{
								Button: pb_busybar.Button_OK,
								Action: pb_busybar.ButtonAction_PRESS,
							},
						},
					},
				},
			},
		},
	}
	payload, _ := proto.Marshal(state)

	before := time.Now().Unix()
	events, err := d.Decode(payload)
	after := time.Now().Unix()

	if err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	ts := events[0].GetTimestamp()
	if ts < before || ts > after {
		t.Errorf("expected timestamp between %d and %d, got %d", before, after, ts)
	}
	if events[0].GetButton().GetTimestamp() != ts {
		t.Errorf("inner button event timestamp %d does not match parent timestamp %d", events[0].GetButton().GetTimestamp(), ts)
	}
}

func TestStreamingWorker(t *testing.T) {
	recorder := &mockMetricsRecorder{}
	d := busybar.NewDecoder("busybar", recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frames := make(chan []byte, 10)
	out := make(chan *pb.NormalizedEvent, 10)

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Run(ctx, frames, out)
	}()

	// Build two test frames with valid button and encoder events
	state1 := &pb_busybar.State{
		Timestamp: 1726180000,
		Updates: []*pb_busybar.StateUpdate{
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_ButtonEvent{
							ButtonEvent: &pb_busybar.ButtonEvent{
								Button: pb_busybar.Button_OK,
								Action: pb_busybar.ButtonAction_PRESS,
							},
						},
					},
				},
			},
		},
	}
	payload1, err := proto.Marshal(state1)
	if err != nil {
		t.Fatalf("failed to marshal frame 1: %v", err)
	}

	state2 := &pb_busybar.State{
		Timestamp: 1726180001,
		Updates: []*pb_busybar.StateUpdate{
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_EncoderEvent{
							EncoderEvent: &pb_busybar.EncoderEvent{
								Delta: 2,
							},
						},
					},
				},
			},
		},
	}
	payload2, err := proto.Marshal(state2)
	if err != nil {
		t.Fatalf("failed to marshal frame 2: %v", err)
	}

	frames <- payload1
	frames <- payload2

	// Read and verify in-order delivery
	select {
	case evt1 := <-out:
		btn := evt1.GetButton()
		if btn == nil || btn.GetButton() != pb.Button_BUTTON_OK || btn.GetAction() != pb.ButtonAction_ACTION_PRESS {
			t.Fatalf("unexpected first event: %v", evt1)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event 1")
	}

	select {
	case evt2 := <-out:
		enc := evt2.GetEncoder()
		if enc == nil || enc.GetDelta() != 2 {
			t.Fatalf("unexpected second event: %v", evt2)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event 2")
	}

	// Cancel context and verify graceful shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("expected context.Canceled or nil, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to stop after context cancellation")
	}
}

func TestStreamingWorker_ChannelSaturation(t *testing.T) {
	recorder := &mockMetricsRecorder{}
	d := busybar.NewDecoder("busybar", recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frames := make(chan []byte, 5)
	// unbuffered out channel that is not read from to trigger saturation
	out := make(chan *pb.NormalizedEvent)

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Run(ctx, frames, out)
	}()

	state := &pb_busybar.State{
		Timestamp: 1726180000,
		Updates: []*pb_busybar.StateUpdate{
			{
				State: &pb_busybar.StateUpdate_Input{
					Input: &pb_busybar.InputEvent{
						Event: &pb_busybar.InputEvent_ButtonEvent{
							ButtonEvent: &pb_busybar.ButtonEvent{
								Button: pb_busybar.Button_OK,
								Action: pb_busybar.ButtonAction_PRESS,
							},
						},
					},
				},
			},
		},
	}
	payload, _ := proto.Marshal(state)
	frames <- payload

	// Wait briefly for Run to attempt dispatch and trigger drop
	deadline := time.Now().Add(500 * time.Millisecond)
	dropped := 0
	for time.Now().Before(deadline) {
		recorder.mu.Lock()
		dropped = recorder.droppedFrameCalls
		recorder.mu.Unlock()
		if dropped > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if dropped == 0 {
		t.Errorf("expected RecordDroppedFrame to be called when out channel is saturated, got %d", dropped)
	}

	cancel()
	<-errCh
}

func TestHomeAssistantJSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		event    *pb.NormalizedEvent
		expected map[string]interface{}
	}{
		{
			name: "Button OK Press",
			event: &pb.NormalizedEvent{
				Device:    "busybar",
				Timestamp: 1726180000,
				Event: &pb.NormalizedEvent_Button{
					Button: &pb.NormalizedButtonEvent{
						Device:    "busybar",
						Button:    pb.Button_BUTTON_OK,
						Action:    pb.ButtonAction_ACTION_PRESS,
						Timestamp: 1726180000,
					},
				},
			},
			expected: map[string]interface{}{
				"device":    "busybar",
				"type":      "button",
				"button":    "ok",
				"action":    "press",
				"timestamp": float64(1726180000),
			},
		},
		{
			name: "Button Back Release",
			event: &pb.NormalizedEvent{
				Device:    "busybar",
				Timestamp: 1726180001,
				Event: &pb.NormalizedEvent_Button{
					Button: &pb.NormalizedButtonEvent{
						Device:    "busybar",
						Button:    pb.Button_BUTTON_BACK,
						Action:    pb.ButtonAction_ACTION_RELEASE,
						Timestamp: 1726180001,
					},
				},
			},
			expected: map[string]interface{}{
				"device":    "busybar",
				"type":      "button",
				"button":    "back",
				"action":    "release",
				"timestamp": float64(1726180001),
			},
		},
		{
			name: "Button Start Press",
			event: &pb.NormalizedEvent{
				Device:    "busybar",
				Timestamp: 1726180002,
				Event: &pb.NormalizedEvent_Button{
					Button: &pb.NormalizedButtonEvent{
						Device:    "busybar",
						Button:    pb.Button_BUTTON_START,
						Action:    pb.ButtonAction_ACTION_PRESS,
						Timestamp: 1726180002,
					},
				},
			},
			expected: map[string]interface{}{
				"device":    "busybar",
				"type":      "button",
				"button":    "start",
				"action":    "press",
				"timestamp": float64(1726180002),
			},
		},
		{
			name: "Switch Busy",
			event: &pb.NormalizedEvent{
				Device:    "busybar",
				Timestamp: 1726180003,
				Event: &pb.NormalizedEvent_Switch{
					Switch: &pb.NormalizedSwitchEvent{
						Device:    "busybar",
						Position:  pb.SwitchPosition_SWITCH_BUSY,
						Timestamp: 1726180003,
					},
				},
			},
			expected: map[string]interface{}{
				"device":    "busybar",
				"type":      "switch",
				"position":  "busy",
				"timestamp": float64(1726180003),
			},
		},
		{
			name: "Switch Custom",
			event: &pb.NormalizedEvent{
				Device:    "busybar",
				Timestamp: 1726180004,
				Event: &pb.NormalizedEvent_Switch{
					Switch: &pb.NormalizedSwitchEvent{
						Device:    "busybar",
						Position:  pb.SwitchPosition_SWITCH_CUSTOM,
						Timestamp: 1726180004,
					},
				},
			},
			expected: map[string]interface{}{
				"device":    "busybar",
				"type":      "switch",
				"position":  "custom",
				"timestamp": float64(1726180004),
			},
		},
		{
			name: "Switch Off",
			event: &pb.NormalizedEvent{
				Device:    "busybar",
				Timestamp: 1726180005,
				Event: &pb.NormalizedEvent_Switch{
					Switch: &pb.NormalizedSwitchEvent{
						Device:    "busybar",
						Position:  pb.SwitchPosition_SWITCH_OFF,
						Timestamp: 1726180005,
					},
				},
			},
			expected: map[string]interface{}{
				"device":    "busybar",
				"type":      "switch",
				"position":  "off",
				"timestamp": float64(1726180005),
			},
		},
		{
			name: "Switch Apps",
			event: &pb.NormalizedEvent{
				Device:    "busybar",
				Timestamp: 1726180006,
				Event: &pb.NormalizedEvent_Switch{
					Switch: &pb.NormalizedSwitchEvent{
						Device:    "busybar",
						Position:  pb.SwitchPosition_SWITCH_APPS,
						Timestamp: 1726180006,
					},
				},
			},
			expected: map[string]interface{}{
				"device":    "busybar",
				"type":      "switch",
				"position":  "apps",
				"timestamp": float64(1726180006),
			},
		},
		{
			name: "Switch Settings",
			event: &pb.NormalizedEvent{
				Device:    "busybar",
				Timestamp: 1726180007,
				Event: &pb.NormalizedEvent_Switch{
					Switch: &pb.NormalizedSwitchEvent{
						Device:    "busybar",
						Position:  pb.SwitchPosition_SWITCH_SETTINGS,
						Timestamp: 1726180007,
					},
				},
			},
			expected: map[string]interface{}{
				"device":    "busybar",
				"type":      "switch",
				"position":  "settings",
				"timestamp": float64(1726180007),
			},
		},
		{
			name: "Encoder Positive Delta",
			event: &pb.NormalizedEvent{
				Device:    "busybar",
				Timestamp: 1726180008,
				Event: &pb.NormalizedEvent_Encoder{
					Encoder: &pb.NormalizedEncoderEvent{
						Device:    "busybar",
						Delta:     1,
						Timestamp: 1726180008,
					},
				},
			},
			expected: map[string]interface{}{
				"device":    "busybar",
				"type":      "encoder",
				"delta":     float64(1),
				"timestamp": float64(1726180008),
			},
		},
		{
			name: "Encoder Negative Delta",
			event: &pb.NormalizedEvent{
				Device:    "busybar",
				Timestamp: 1726180009,
				Event: &pb.NormalizedEvent_Encoder{
					Encoder: &pb.NormalizedEncoderEvent{
						Device:    "busybar",
						Delta:     -3,
						Timestamp: 1726180009,
					},
				},
			},
			expected: map[string]interface{}{
				"device":    "busybar",
				"type":      "encoder",
				"delta":     float64(-3),
				"timestamp": float64(1726180009),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Test helper on event
			data, err := tc.event.ToHomeAssistantJSON()
			if err != nil {
				t.Fatalf("event.ToHomeAssistantJSON() failed: %v", err)
			}

			var actual map[string]interface{}
			if err := json.Unmarshal(data, &actual); err != nil {
				t.Fatalf("failed to unmarshal generated JSON: %v", err)
			}

			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("JSON mismatch:\nGot:  %#v\nWant: %#v", actual, tc.expected)
			}

			// Test helper in busybar package
			data2, err := busybar.ToHomeAssistantJSON(tc.event)
			if err != nil {
				t.Fatalf("busybar.ToHomeAssistantJSON() failed: %v", err)
			}
			var actual2 map[string]interface{}
			if err := json.Unmarshal(data2, &actual2); err != nil {
				t.Fatalf("failed to unmarshal generated JSON from package function: %v", err)
			}
			if !reflect.DeepEqual(actual2, tc.expected) {
				t.Errorf("package function JSON mismatch:\nGot:  %#v\nWant: %#v", actual2, tc.expected)
			}
		})
	}
}
