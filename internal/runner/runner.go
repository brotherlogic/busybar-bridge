package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/brotherlogic/busybar-bridge/internal/busybar"
	"github.com/brotherlogic/busybar-bridge/internal/calendar"
	"github.com/brotherlogic/busybar-bridge/internal/config"
	"github.com/brotherlogic/busybar-bridge/internal/hass"
	"github.com/brotherlogic/busybar-bridge/internal/server"
	"github.com/brotherlogic/busybar-bridge/internal/telemetry"
	"github.com/brotherlogic/busybar-bridge/pkg/pb"
)

// Runner manages the runtime lifecycle and event pipeline wiring across
// telemetry, Busy Bar ingestion, Protobuf decoding, Home Assistant dispatching,
// and the HTTP observability server.
type Runner struct {
	cfg           *config.AppConfig
	store         *telemetry.Store
	busyClient    *busybar.Client
	decoder       *busybar.Decoder
	hassClient    *hass.Client
	server        *server.Server
	calendarStore *calendar.Store
	calendarMgr   *calendar.Manager

	mu      sync.Mutex
	running bool
}

// Option configures Runner dependencies.
type Option func(*Runner)

// WithStore overrides the telemetry store.
func WithStore(store *telemetry.Store) Option {
	return func(r *Runner) {
		r.store = store
	}
}

// WithBusyBarClient overrides the Busy Bar client.
func WithBusyBarClient(client *busybar.Client) Option {
	return func(r *Runner) {
		r.busyClient = client
	}
}

// WithDecoder overrides the event decoder.
func WithDecoder(decoder *busybar.Decoder) Option {
	return func(r *Runner) {
		r.decoder = decoder
	}
}

// WithHassClient overrides the Home Assistant client.
func WithHassClient(client *hass.Client) Option {
	return func(r *Runner) {
		r.hassClient = client
	}
}

// WithServer overrides the HTTP server.
func WithServer(srv *server.Server) Option {
	return func(r *Runner) {
		r.server = srv
	}
}

// WithCalendarStore overrides the calendar store.
func WithCalendarStore(store *calendar.Store) Option {
	return func(r *Runner) {
		r.calendarStore = store
	}
}

// WithCalendarManager overrides the Google OAuth manager.
func WithCalendarManager(mgr *calendar.Manager) Option {
	return func(r *Runner) {
		r.calendarMgr = mgr
	}
}

// WithOAuthManager overrides the Google OAuth manager (alias for WithCalendarManager).
func WithOAuthManager(mgr *calendar.Manager) Option {
	return WithCalendarManager(mgr)
}

// NewRunner constructs and validates a new Runner instance with configured components.
func NewRunner(cfg *config.AppConfig, opts ...Option) (*Runner, error) {
	if cfg == nil {
		return nil, errors.New("app config cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid app config: %w", err)
	}

	calStorePath := cfg.CalendarStorePath
	if calStorePath == "" {
		calStorePath = config.DefaultConfig().CalendarStorePath
	}
	calStore, err := calendar.NewStore(calStorePath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize calendar store: %w", err)
	}

	var calMgr *calendar.Manager
	if cfg.IsOAuthConfigured() {
		calMgr = calendar.NewManager(calendar.ManagerConfig{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			RedirectURL:  cfg.GoogleRedirectURL,
		})
	}

	store := telemetry.NewStore()

	busyCfg := busybar.DefaultConfig()
	busyCfg.Host = cfg.BusyBarHost
	busyCfg.Port = cfg.BusyBarPort
	busyClient := busybar.NewClient(busyCfg)

	decoder := busybar.NewDecoder(cfg.BusyBarDeviceID, nil)

	hassCfg := hass.DefaultConfig()
	hassCfg.BaseURL = cfg.HassURL
	hassCfg.Token = cfg.HassToken
	hassCfg.EventType = cfg.HassEventType
	hassCfg.RequestTimeout = cfg.HassTimeout
	hassClient, err := hass.NewClient(hassCfg, store)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Home Assistant client: %w", err)
	}

	r := &Runner{
		cfg:           cfg,
		store:         store,
		busyClient:    busyClient,
		decoder:       decoder,
		hassClient:    hassClient,
		calendarStore: calStore,
		calendarMgr:   calMgr,
	}

	for _, opt := range opts {
		opt(r)
	}

	if r.server == nil {
		serverCfg := server.DefaultConfig()
		serverCfg.Port = cfg.Port

		var serverOpts []server.ServerOption
		if r.calendarStore != nil {
			serverOpts = append(serverOpts, server.WithCalendarStore(r.calendarStore))
		}
		if r.calendarMgr != nil {
			serverOpts = append(serverOpts, server.WithCalendarManager(r.calendarMgr))
		}
		serverOpts = append(serverOpts, server.WithOAuthConfigured(cfg.IsOAuthConfigured()))

		r.server = server.NewServer(serverCfg, r.store, serverOpts...)
	}

	return r, nil
}

// Config returns the active application configuration.
func (r *Runner) Config() *config.AppConfig {
	return r.cfg
}

// Store returns the associated telemetry store.
func (r *Runner) Store() *telemetry.Store {
	return r.store
}

// BusyBarClient returns the Busy Bar WebSocket client.
func (r *Runner) BusyBarClient() *busybar.Client {
	return r.busyClient
}

// Decoder returns the Protobuf event decoder.
func (r *Runner) Decoder() *busybar.Decoder {
	return r.decoder
}

// HassClient returns the Home Assistant REST client.
func (r *Runner) HassClient() *hass.Client {
	return r.hassClient
}

// Server returns the HTTP observability server.
func (r *Runner) Server() *server.Server {
	return r.server
}

// CalendarStore returns the calendar persistence store.
func (r *Runner) CalendarStore() *calendar.Store {
	return r.calendarStore
}

// CalendarManager returns the Google OAuth manager, or nil if not configured.
func (r *Runner) CalendarManager() *calendar.Manager {
	return r.calendarMgr
}

// OAuthManager returns the Google OAuth manager, or nil if not configured.
func (r *Runner) OAuthManager() *calendar.Manager {
	return r.calendarMgr
}

// TranslateEvent converts a normalized Protobuf event into a typed Home Assistant Event.
func TranslateEvent(norm *pb.NormalizedEvent) (hass.Event, bool) {
	if norm == nil || norm.GetEvent() == nil {
		return hass.Event{}, false
	}

	device := norm.GetDevice()
	if device == "" {
		device = "busybar"
	}
	ts := norm.GetTimestamp()
	if ts <= 0 {
		ts = time.Now().Unix()
	}

	switch evt := norm.GetEvent().(type) {
	case *pb.NormalizedEvent_Button:
		if evt.Button == nil {
			return hass.Event{}, false
		}
		btnDev := device
		if d := evt.Button.GetDevice(); d != "" {
			btnDev = d
		}
		var btnStr string
		switch evt.Button.GetButton() {
		case pb.Button_BUTTON_OK:
			btnStr = "ok"
		case pb.Button_BUTTON_BACK:
			btnStr = "back"
		case pb.Button_BUTTON_START:
			btnStr = "start"
		default:
			btnStr = "unknown"
		}

		var actStr string
		switch evt.Button.GetAction() {
		case pb.ButtonAction_ACTION_PRESS:
			actStr = "press"
		case pb.ButtonAction_ACTION_RELEASE:
			actStr = "release"
		default:
			actStr = "unknown"
		}

		return hass.NewButtonEvent(btnDev, btnStr, actStr, ts), true

	case *pb.NormalizedEvent_Switch:
		if evt.Switch == nil {
			return hass.Event{}, false
		}
		swDev := device
		if d := evt.Switch.GetDevice(); d != "" {
			swDev = d
		}
		var posStr string
		switch evt.Switch.GetPosition() {
		case pb.SwitchPosition_SWITCH_BUSY:
			posStr = "busy"
		case pb.SwitchPosition_SWITCH_CUSTOM:
			posStr = "custom"
		case pb.SwitchPosition_SWITCH_OFF:
			posStr = "off"
		case pb.SwitchPosition_SWITCH_APPS:
			posStr = "apps"
		case pb.SwitchPosition_SWITCH_SETTINGS:
			posStr = "settings"
		default:
			posStr = "unknown"
		}

		return hass.NewSwitchEvent(swDev, posStr, ts), true

	case *pb.NormalizedEvent_Encoder:
		if evt.Encoder == nil {
			return hass.Event{}, false
		}
		encDev := device
		if d := evt.Encoder.GetDevice(); d != "" {
			encDev = d
		}
		delta := int(evt.Encoder.GetDelta())

		return hass.NewEncoderEvent(encDev, delta, ts), true

	default:
		return hass.Event{}, false
	}
}

func formatSummary(ev hass.Event) string {
	switch ev.Type {
	case hass.EventTypeButton:
		btn, _ := ev.Data["button"].(string)
		act, _ := ev.Data["action"].(string)
		return fmt.Sprintf("button %s %s", btn, act)
	case hass.EventTypeSwitch:
		pos, _ := ev.Data["position"].(string)
		return fmt.Sprintf("switch %s", pos)
	case hass.EventTypeEncoder:
		delta, _ := ev.Data["delta"]
		return fmt.Sprintf("encoder %v", delta)
	default:
		return string(ev.Type)
	}
}

// Run executes the application runtime pipeline, starts services, superintends event ingestion,
// and coordinates graceful shutdown. Returns nil on clean shutdown.
func (r *Runner) Run(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return errors.New("runner is already running")
	}
	r.running = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	// 1. Start HTTP server
	if r.server != nil {
		if err := r.server.Start(ctx); err != nil {
			return fmt.Errorf("failed to start HTTP server: %w", err)
		}
	}

	// 2. Start Home Assistant dispatcher
	if r.hassClient != nil {
		if err := r.hassClient.Start(ctx); err != nil {
			return fmt.Errorf("failed to start Home Assistant dispatcher: %w", err)
		}
	}

	// 3. Start Busy Bar client
	if r.busyClient != nil {
		if err := r.busyClient.Start(ctx); err != nil {
			return fmt.Errorf("failed to start Busy Bar client: %w", err)
		}
	}

	// Periodically sync connection state from client to telemetry store
	syncCtx, syncCancel := context.WithCancel(ctx)
	defer syncCancel()

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-syncCtx.Done():
				return
			case <-ticker.C:
				if r.store != nil && r.busyClient != nil {
					r.store.SetConnected(r.busyClient.IsConnected())
				}
			}
		}
	}()

	// Pipeline frame ingestion loop
	var framesChan <-chan []byte
	if r.busyClient != nil {
		framesChan = r.busyClient.Frames()
	}

frameLoop:
	for {
		select {
		case <-ctx.Done():
			break frameLoop
		case frame, ok := <-framesChan:
			if !ok {
				break frameLoop
			}
			r.handleFrame(frame)
		}
	}

	// --- Graceful Shutdown Sequence ---
	syncCancel()
	if r.store != nil {
		r.store.SetConnected(false)
	}

	// Step 1: Close WebSocket client to cease incoming frames
	if r.busyClient != nil {
		_ = r.busyClient.Close()
	}

	// Drain any remaining frames left in the buffered channel
	if framesChan != nil {
	drainLoop:
		for {
			select {
			case frame, ok := <-framesChan:
				if !ok {
					break drainLoop
				}
				r.handleFrame(frame)
			default:
				break drainLoop
			}
		}
	}

	// Step 2: Flush queued events through Home Assistant dispatcher bounded by ShutdownTimeout
	shutdownTimeout := r.cfg.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = 10 * time.Second
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	hassClosed := make(chan error, 1)
	go func() {
		if r.hassClient != nil {
			hassClosed <- r.hassClient.Close()
		} else {
			hassClosed <- nil
		}
	}()

	select {
	case <-hassClosed:
	case <-shutdownCtx.Done():
	}

	// Step 3: Shutdown HTTP server
	if r.server != nil {
		_ = r.server.Shutdown(shutdownCtx)
	}

	// Step 4: Clean return on normal shutdown
	return nil
}

func (r *Runner) handleFrame(frame []byte) {
	if r.decoder == nil {
		return
	}

	events, err := r.decoder.Decode(frame)
	if err != nil {
		return
	}

	for _, normEvt := range events {
		hassEvt, ok := TranslateEvent(normEvt)
		if !ok {
			continue
		}

		if r.store != nil {
			summary := formatSummary(hassEvt)
			traceID := r.store.RecordEvent(string(hassEvt.Type), summary)
			hassEvt.TraceID = traceID
		}

		if r.hassClient != nil {
			r.hassClient.Enqueue(hassEvt)
		}
	}

	if r.store != nil && r.busyClient != nil {
		r.store.SetConnected(r.busyClient.IsConnected())
	}
}
