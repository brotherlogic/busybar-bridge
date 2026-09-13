package busybar

// MetricsRecorder defines diagnostic instrumentation methods for event decoding and frame handling.
type MetricsRecorder interface {
	RecordDecodeSuccess(eventCount int)
	RecordDecodeError()
	RecordDroppedFrame()
}

// NoopMetricsRecorder provides a no-op implementation of MetricsRecorder.
type NoopMetricsRecorder struct{}

// RecordDecodeSuccess is a no-op recorder implementation.
func (NoopMetricsRecorder) RecordDecodeSuccess(eventCount int) {}

// RecordDecodeError is a no-op recorder implementation.
func (NoopMetricsRecorder) RecordDecodeError() {}

// RecordDroppedFrame is a no-op recorder implementation.
func (NoopMetricsRecorder) RecordDroppedFrame() {}
