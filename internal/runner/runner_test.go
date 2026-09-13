package runner_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/busybar"
	"github.com/brotherlogic/busybar-bridge/internal/config"
	"github.com/brotherlogic/busybar-bridge/internal/hass"
	"github.com/brotherlogic/busybar-bridge/internal/runner"
	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
	"github.com/brotherlogic/busybar-bridge/pkg/pb"
	pb_busybar "github.com/brotherlogic/busybar-bridge/pkg/pb/busybar"
	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

func TestNewRunner_Valid(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.HassToken = "valid-token"

	r, err := runner.NewRunner(cfg)
	if err != nil {
		t.Fatalf("expected NewRunner to succeed, got %v", err)
	}
	if r == nil {
		t.Fatalf("expected non-nil runner")
	}
	if r.Config() != cfg {
		t.Errorf("expected Config to match input cfg")
	}
	if r.Store() == nil {
		t.Errorf("expected non-nil telemetry Store")
	}
	if r.BusyBarClient() == nil {
		t.Errorf("expected non-nil BusyBarClient")
	}
	if r.Decoder() == nil {
		t.Errorf("expected non-nil Decoder")
	}
	if r.HassClient() == nil {
		t.Errorf("expected non-nil HassClient")
	}
	if r.Server() == nil {
		t.Errorf("expected non-nil Server")
	}
}

func TestNewRunner_InvalidConfig(t *testing.T) {
	_, err := runner.NewRunner(nil)
	if err == nil {
		t.Errorf("expected error for nil config")
	}

	invalidCfg := config.DefaultConfig()
	invalidCfg.HassToken = "" // missing token
	_, err = runner.NewRunner(invalidCfg)
	if err == nil {
		t.Errorf("expected error for invalid config missing token")
	}
}

func TestTranslateEvent(t *testing.T) {
	tests := []struct {
		name          string
		input         *pb.NormalizedEvent
		expectedType  hass.EventType
		expectedValid bool
		checkData     func(t *testing.T, ev hass.Event)
	}{
		{
			name: "Button Event",
			input: &pb.NormalizedEvent{
				Device:    "device-1",
				Timestamp: 1726000001,
				Event: &pb.NormalizedEvent_Button{
					Button: &pb.NormalizedButtonEvent{
						Device:    "device-1",
						Button:    pb.Button_BUTTON_OK,
						Action:    pb.ButtonAction_ACTION_PRESS,
						Timestamp: 1726000001,
					},
				},
			},
			expectedType:  hass.EventTypeButton,
			expectedValid: true,
			checkData: func(t *testing.T, ev hass.Event) {
				if ev.Device != "device-1" {
					t.Errorf("expected device-1, got %q", ev.Device)
				}
				if ev.Timestamp != 1726000001 {
					t.Errorf("expected ts 1726000001, got %d", ev.Timestamp)
				}
				btn, err := ev.ToButtonPayload()
				if err != nil {
					t.Fatalf("failed to parse button payload: %v", err)
				}
				if btn.Button != "ok" || btn.Action != "press" {
					t.Errorf("expected button ok press, got %s %s", btn.Button, btn.Action)
				}
			},
		},
		{
			name: "Switch Event",
			input: &pb.NormalizedEvent{
				Device:    "device-2",
				Timestamp: 1726000002,
				Event: &pb.NormalizedEvent_Switch{
					Switch: &pb.NormalizedSwitchEvent{
						Device:    "device-2",
						Position:  pb.SwitchPosition_SWITCH_BUSY,
						Timestamp: 1726000002,
					},
				},
			},
			expectedType:  hass.EventTypeSwitch,
			expectedValid: true,
			checkData: func(t *testing.T, ev hass.Event) {
				sw, err := ev.ToSwitchPayload()
				if err != nil {
					t.Fatalf("failed to parse switch payload: %v", err)
				}
				if sw.Position != "busy" {
					t.Errorf("expected switch position busy, got %q", sw.Position)
				}
			},
		},
		{
			name: "Encoder Event",
			input: &pb.NormalizedEvent{
				Device:    "device-3",
				Timestamp: 1726000003,
				Event: &pb.NormalizedEvent_Encoder{
					Encoder: &pb.NormalizedEncoderEvent{
						Device:    "device-3",
						Delta:     -2,
						Timestamp: 1726000003,
					},
				},
			},
			expectedType:  hass.EventTypeEncoder,
			expectedValid: true,
			checkData: func(t *testing.T, ev hass.Event) {
				enc, err := ev.ToEncoderPayload()
				if err != nil {
					t.Fatalf("failed to parse encoder payload: %v", err)
				}
				if enc.Delta != -2 {
					t.Errorf("expected delta -2, got %d", enc.Delta)
				}
			},
		},
		{
			name:          "Nil event",
			input:         nil,
			expectedValid: false,
		},
		{
			name: "Empty event wrapper",
			input: &pb.NormalizedEvent{
				Device:    "device-empty",
				Timestamp: 1726000004,
			},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, ok := runner.TranslateEvent(tt.input)
			if ok != tt.expectedValid {
				t.Fatalf("expected valid=%v, got %v", tt.expectedValid, ok)
			}
			if !ok {
				return
			}
			if ev.Type != tt.expectedType {
				t.Errorf("expected type %v, got %v", tt.expectedType, ev.Type)
			}
			if tt.checkData != nil {
				tt.checkData(t, ev)
			}
		})
	}
}

func TestRunner_PipelineIntegrationAndGracefulShutdown(t *testing.T) {
	// 1. Mock Home Assistant Server
	var receivedCount atomic.Int32
	var receivedMu sync.Mutex
	var receivedEvents []hass.Event

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-secret-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var ev hass.Event
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		receivedMu.Lock()
		receivedEvents = append(receivedEvents, ev)
		receivedMu.Unlock()
		receivedCount.Add(1)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "Event announced."}`))
	}))
	defer haServer.Close()

	// 2. Mock Busy Bar WebSocket Server
	stateMsg := &pb_busybar.State{
		Timestamp: uint64(time.Now().Unix()),
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
							EncoderEvent: &pb_busybar.EncoderEvent{
								Delta: 1,
							},
						},
					},
				},
			},
		},
	}
	stateBytes, err := proto.Marshal(stateMsg)
	if err != nil {
		t.Fatalf("failed to marshal state message: %v", err)
	}

	var wsConnected atomic.Bool
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status/ws" {
			http.NotFound(w, r)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "closing")
		wsConnected.Store(true)

		// Read activation message {"enable": true}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, _, err = c.Read(ctx)
		if err != nil {
			return
		}

		// Write the binary state frame
		writeCtx, writeCancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer writeCancel()
		_ = c.Write(writeCtx, websocket.MessageBinary, stateBytes)

		// Keep connection alive until context cancels
		<-r.Context().Done()
	}))
	defer wsServer.Close()

	// Parse ws host and port
	wsHost, wsPortStr, err := net.SplitHostPort(wsServer.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to split ws addr: %v", err)
	}
	wsPort, _ := strconv.Atoi(wsPortStr)

	// Find a free port for HTTP server
	freeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	_, serverPortStr, err := net.SplitHostPort(freeLn.Addr().String())
	if err != nil {
		t.Fatalf("failed to split free port: %v", err)
	}
	serverPort, _ := strconv.Atoi(serverPortStr)
	_ = freeLn.Close()

	// 3. Configure runner with dynamic port
	cfg := &config.AppConfig{
		BusyBarHost:        wsHost,
		BusyBarPort:        wsPort,
		BusyBarDeviceID:    "busybar-test",
		HassURL:            haServer.URL,
		HassToken:          "test-secret-token",
		HassEventType:      "busybar_event",
		HassTimeout:        2 * time.Second,
		ForwardStateEvents: false,
		Port:               serverPort,
		ShutdownTimeout:    3 * time.Second,
		LogLevel:           "info",
	}

	r, err := runner.NewRunner(cfg)
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() {
		runDone <- r.Run(ctx)
	}()

	// 4. Verify pipeline receives events and dispatches to HA
	deadline := time.Now().Add(5 * time.Second)
	for receivedCount.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if receivedCount.Load() < 3 {
		t.Fatalf("expected at least 3 events received by Home Assistant, got %d", receivedCount.Load())
	}

	// 5. Verify telemetry store status and counters
	snap := r.Store().Snapshot()
	if snap.Counters.Buttons < 1 {
		t.Errorf("expected at least 1 button recorded in telemetry, got %d", snap.Counters.Buttons)
	}
	if snap.Counters.Switches < 1 {
		t.Errorf("expected at least 1 switch recorded in telemetry, got %d", snap.Counters.Switches)
	}
	if snap.Counters.Encoders < 1 {
		t.Errorf("expected at least 1 encoder recorded in telemetry, got %d", snap.Counters.Encoders)
	}

	// 6. Verify HTTP server observability endpoints
	serverAddr := r.Server().Addr()
	if serverAddr == "" {
		t.Fatalf("server Addr is empty")
	}

	// Test /healthz
	resp, err := http.Get("http://" + serverAddr + "/healthz")
	if err != nil {
		t.Fatalf("failed to call /healthz: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz expected 200, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Test /ready (should be 200 when connected)
	resp, err = http.Get("http://" + serverAddr + "/ready")
	if err != nil {
		t.Fatalf("failed to call /ready: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/ready expected 200 OK when connected, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 7. Verify graceful shutdown
	cancel() // Trigger context cancellation

	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("expected nil error on normal shutdown, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Run() did not shut down within timeout")
	}

	// After shutdown, connection status in telemetry store should be false
	if r.Store().IsConnected() {
		t.Errorf("expected telemetry store IsConnected() to be false after shutdown")
	}
}

func TestRunner_ServerStartError(t *testing.T) {
	// Occupy a port first to force a collision
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on port: %v", err)
	}
	defer ln.Close()

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	cfg := config.DefaultConfig()
	cfg.HassToken = "token"
	cfg.Port = port // collides with ln

	r, err := runner.NewRunner(cfg)
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = r.Run(ctx)
	if err == nil {
		t.Fatalf("expected error starting server with occupied port, got nil")
	}
}

func TestRunner_Options(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.HassToken = "test-token"

	customStore := telemetry.NewStore()
	customDecoder := busybar.NewDecoder("custom-dev", nil)

	r, err := runner.NewRunner(cfg,
		runner.WithStore(customStore),
		runner.WithDecoder(customDecoder),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if r.Store() != customStore {
		t.Errorf("expected custom store to be set")
	}
	if r.Decoder() != customDecoder {
		t.Errorf("expected custom decoder to be set")
	}
}

func TestRunner_AlreadyRunningError(t *testing.T) {
	freeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(freeLn.Addr().String())
	port, _ := strconv.Atoi(portStr)
	_ = freeLn.Close()

	cfg := config.DefaultConfig()
	cfg.HassToken = "token"
	cfg.Port = port

	r, err := runner.NewRunner(cfg)
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() {
		runDone <- r.Run(ctx)
	}()

	// Give a moment to start
	time.Sleep(50 * time.Millisecond)

	// Attempt concurrent run
	err2 := r.Run(ctx)
	if err2 == nil || err2.Error() != "runner is already running" {
		t.Errorf("expected 'runner is already running' error, got %v", err2)
	}

	cancel()
	<-runDone
}

func TestRunner_StartErrors_Hass(t *testing.T) {
	freeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(freeLn.Addr().String())
	port, _ := strconv.Atoi(portStr)
	_ = freeLn.Close()

	cfg := config.DefaultConfig()
	cfg.HassToken = "token"
	cfg.Port = port

	r, err := runner.NewRunner(cfg)
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	// Pre-start hass client to induce ErrAlreadyStarted on Run
	ctx := context.Background()
	_ = r.HassClient().Start(ctx)

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = r.Run(runCtx)
	if err == nil {
		t.Fatalf("expected error from Run when HassClient is already started, got nil")
	}
}

func TestRunner_StartErrors_BusyBar(t *testing.T) {
	freeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(freeLn.Addr().String())
	port, _ := strconv.Atoi(portStr)
	_ = freeLn.Close()

	cfg := config.DefaultConfig()
	cfg.HassToken = "token"
	cfg.Port = port

	r, err := runner.NewRunner(cfg)
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	// Pre-start BusyBar client to induce error on Run
	ctx := context.Background()
	_ = r.BusyBarClient().Start(ctx)

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = r.Run(runCtx)
	if err == nil {
		t.Fatalf("expected error from Run when BusyBarClient is already started, got nil")
	}
}

func TestRunner_ShutdownTimeout(t *testing.T) {
	freeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(freeLn.Addr().String())
	port, _ := strconv.Atoi(portStr)
	_ = freeLn.Close()

	cfg := config.DefaultConfig()
	cfg.HassToken = "token"
	cfg.Port = port
	cfg.ShutdownTimeout = 50 * time.Millisecond // Short timeout

	r, err := runner.NewRunner(cfg)
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- r.Run(ctx)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("expected clean shutdown even with short timeout, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Run() did not finish within expected bounds")
	}
}
