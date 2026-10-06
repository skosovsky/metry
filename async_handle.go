package metry

import (
	"context"
	"errors"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/async"
	"github.com/skosovsky/metry/internal/traceutil"
)

const defaultAsyncTracerName = "metry.async"

// ErrNilProvider is returned when a Provider method is called on a nil provider.
var ErrNilProvider = errors.New("metry: provider is nil")

// ErrNilTracerProvider is returned when a provider has no tracer configured.
var ErrNilTracerProvider = errors.New("metry: tracer provider is nil")

// AsyncHandle references an originating interaction or explicit disabled telemetry.
// Real references serialize; disabled handles cannot produce an origin token.
type AsyncHandle struct {
	h async.Handle
}

// ErrNoSpanContext is returned when NewAsyncHandle is called without a valid span in context.
var ErrNoSpanContext = async.ErrNoSpanContext

// ErrInvalidAsyncHandle is returned when an async handle token cannot be parsed.
var ErrInvalidAsyncHandle = async.ErrInvalidHandle

// ErrHandleTokenTooLarge is returned when an async handle token exceeds the size limit.
var ErrHandleTokenTooLarge = async.ErrHandleTokenTooLarge

// NewAsyncHandle captures the current span context from ctx as a portable handle.
// Marshal tokens are not signed; treat queue payloads as trusted or add application-level signing.
func NewAsyncHandle(ctx context.Context) (AsyncHandle, error) {
	h, err := async.NewHandle(ctx)
	if err != nil {
		return AsyncHandle{}, err
	}
	return AsyncHandle{h: h}, nil
}

// ParseAsyncHandle decodes a token produced by AsyncHandle.Marshal.
func ParseAsyncHandle(token string) (AsyncHandle, error) {
	h, err := async.ParseHandle(token)
	if err != nil {
		return AsyncHandle{}, err
	}
	return AsyncHandle{h: h}, nil
}

// NoopAsyncHandle returns a disabled handle without a trace identity.
// Linked recording is dropped, IsValid is false and Marshal returns an error.
func NoopAsyncHandle() AsyncHandle {
	return AsyncHandle{h: async.NewNoopHandle()}
}

// LinkedSpanWriter configures a linked span without exposing OpenTelemetry types.
// Copies share state. Concurrent use is supported only while the callback runs;
// join all workers before returning. Writes after callback completion are inert.
type LinkedSpanWriter struct{ state *linkedSpanWriterState }

type linkedSpanWriterState struct {
	mu        sync.Mutex
	span      trace.Span
	active    bool
	statusErr error
}

// SetAttributes sets span attributes from typed metry attributes.
func (w LinkedSpanWriter) SetAttributes(attrs ...Attribute) {
	if w.state == nil {
		return
	}
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	if !w.state.active {
		return
	}
	traceutil.MutateRecordingSpan(w.state.span, func(span trace.Span) {
		if kv := attributesToOTel(attrs); len(kv) > 0 {
			span.SetAttributes(kv...)
		}
	})
}

// AddEvent adds a named event with typed metry attributes.
func (w LinkedSpanWriter) AddEvent(name string, attrs ...Attribute) {
	if w.state == nil {
		return
	}
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	if !w.state.active {
		return
	}
	traceutil.MutateRecordingSpan(w.state.span, func(span trace.Span) {
		span.AddEvent(name, trace.WithAttributes(attributesToOTel(attrs)...))
	})
}

// SetError retains the first non-nil error without making the callback fail.
// Nil is a no-op. A callback return error takes precedence over this status.
func (w LinkedSpanWriter) SetError(err error) {
	if w.state == nil || err == nil {
		return
	}
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	if w.state.active && w.state.statusErr == nil {
		w.state.statusErr = err
	}
}

func (w LinkedSpanWriter) close() error {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	w.state.active = false
	return w.state.statusErr
}

// Marshal encodes the handle as a portable string token.
func (h AsyncHandle) Marshal() (string, error) {
	return h.h.Marshal()
}

// IsValid reports whether the handle references a valid span context.
func (h AsyncHandle) IsValid() bool {
	return h.h.IsValid()
}

// LinkedSpanOption configures linked span start behavior without exposing OTel types.
type LinkedSpanOption = func(*linkedSpanConfig)

type linkedSpanConfig struct {
	otelOpts []trace.SpanStartOption
}

// WithLinkedAttributes adds typed attributes at linked span start.
func WithLinkedAttributes(attrs ...Attribute) LinkedSpanOption {
	return func(c *linkedSpanConfig) {
		kv := attributesToOTel(attrs)
		if len(kv) > 0 {
			c.otelOpts = append(c.otelOpts, trace.WithAttributes(kv...))
		}
	}
}

func linkedSpanOptionsToOTel(opts ...LinkedSpanOption) []trace.SpanStartOption {
	var cfg linkedSpanConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg.otelOpts
}

// RecordLinkedSpan starts a linked root span and runs fn with a typed writer.
// The callback must join concurrent writer workers before returning. Disabled
// handles do not call fn or create telemetry; callbacks must not own business work.
func (h AsyncHandle) RecordLinkedSpan(
	ctx context.Context,
	provider *Provider,
	name string,
	fn func(LinkedSpanWriter) error,
	opts ...LinkedSpanOption,
) error {
	if h.h.IsDisabled() {
		return nil
	}
	if !h.IsValid() {
		return ErrInvalidAsyncHandle
	}
	if provider == nil {
		return ErrNilProvider
	}
	if provider.otelTracer == nil {
		return ErrNilTracerProvider
	}
	otelOpts := linkedSpanOptionsToOTel(opts...)
	tracer := provider.tracerProvider().Tracer(defaultAsyncTracerName)
	_, span, err := async.StartLinkedSpan(ctx, tracer, h.h, name, otelOpts...)
	if err != nil {
		return err
	}
	defer span.End()
	defer func() {
		if value := recover(); value != nil {
			traceutil.RecordDiagnostic(span, provider.DiagnosticPolicy().Panic(value))
			span.End()
			panic(value)
		}
	}()

	writer := LinkedSpanWriter{state: &linkedSpanWriterState{
		mu: sync.Mutex{}, span: span, active: true, statusErr: nil,
	}}
	defer func() { _ = writer.close() }()
	callbackErr := fn(writer)
	statusErr := writer.close()
	if callbackErr != nil {
		traceutil.SpanErrorWithPolicy(span, callbackErr, provider.DiagnosticPolicy())
		return callbackErr
	}
	if statusErr != nil {
		traceutil.SpanErrorWithPolicy(span, statusErr, provider.DiagnosticPolicy())
		return nil
	}
	traceutil.SpanOK(span)
	return nil
}

// RecordLinkedOutcomeWithProvider uses the provider tracer to record a linked outcome span.
func (h AsyncHandle) RecordLinkedOutcomeWithProvider(
	ctx context.Context,
	provider *Provider,
	spanName string,
	attrs ...Attribute,
) error {
	if h.h.IsDisabled() {
		return nil
	}
	if !h.IsValid() {
		return ErrInvalidAsyncHandle
	}
	if provider == nil {
		return ErrNilProvider
	}
	if provider.otelTracer == nil {
		return ErrNilTracerProvider
	}
	kv := attributesToOTel(attrs)
	return h.h.RecordLinkedOutcome(
		ctx,
		provider.tracerProvider().Tracer(defaultAsyncTracerName),
		spanName,
		kv,
	)
}

func attributesToOTel(attrs []Attribute) []attribute.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	kv := make([]attribute.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		if attr.Key() == "" {
			continue
		}
		otelKV := attr.toOTel()
		if otelKV.Key == "" {
			continue
		}
		kv = append(kv, otelKV)
	}
	return kv
}
