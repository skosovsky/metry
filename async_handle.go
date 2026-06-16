package metry

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/async"
	"github.com/skosovsky/metry/internal/traceutil"
)

const defaultAsyncTracerName = "metry.async"

// ErrNilProvider is returned when a Provider method is called on a nil provider.
var ErrNilProvider = errors.New("metry: provider is nil")

// ErrInvalidProviderType is returned when a value is not a *Provider.
var ErrInvalidProviderType = errors.New("metry: provider must be *Provider")

// ErrNilTracerProvider is returned when a provider has no tracer configured.
var ErrNilTracerProvider = errors.New("metry: tracer provider is nil")

// ErrNilMeterProvider is returned when a provider has no meter configured.
var ErrNilMeterProvider = errors.New("metry: meter provider is nil")

// AsyncHandle is a serializable token linking deferred outcomes to an originating interaction.
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

// LinkedSpanWriter configures a linked span without exposing OpenTelemetry types.
type LinkedSpanWriter struct {
	setAttrs func(...Attribute)
	addEvent func(name string, attrs ...Attribute)
}

// SetAttributes sets span attributes from typed metry attributes.
func (w LinkedSpanWriter) SetAttributes(attrs ...Attribute) {
	if w.setAttrs != nil {
		w.setAttrs(attrs...)
	}
}

// AddEvent adds a named event with typed metry attributes.
func (w LinkedSpanWriter) AddEvent(name string, attrs ...Attribute) {
	if w.addEvent != nil {
		w.addEvent(name, attrs...)
	}
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
func (h AsyncHandle) RecordLinkedSpan(
	ctx context.Context,
	provider *Provider,
	name string,
	fn func(LinkedSpanWriter) error,
	opts ...LinkedSpanOption,
) error {
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
	return h.h.RunLinkedSpan(ctx, tracer, name, func(span trace.Span) error {
		writer := LinkedSpanWriter{
			setAttrs: func(attrs ...Attribute) {
				traceutil.MutateRecordingSpan(span, func(span trace.Span) {
					kv := attributesToOTel(attrs)
					if len(kv) > 0 {
						span.SetAttributes(kv...)
					}
				})
			},
			addEvent: func(eventName string, attrs ...Attribute) {
				traceutil.MutateRecordingSpan(span, func(span trace.Span) {
					kv := attributesToOTel(attrs)
					span.AddEvent(eventName, trace.WithAttributes(kv...))
				})
			},
		}
		return fn(writer)
	}, otelOpts...)
}

// RecordLinkedOutcomeWithProvider uses the provider tracer to record a linked outcome span.
func (h AsyncHandle) RecordLinkedOutcomeWithProvider(
	ctx context.Context,
	provider *Provider,
	spanName string,
	attrs ...Attribute,
) error {
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
