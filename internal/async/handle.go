package async

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/trace"
)

const maxHandleTokenLen = 512

// ErrNoSpanContext is returned when NewHandle is called without a valid span in context.
var ErrNoSpanContext = errors.New("metry/async: no valid span context in context")

// ErrInvalidHandle is returned when a handle token cannot be parsed.
var ErrInvalidHandle = errors.New("metry/async: invalid async handle")

// ErrHandleTokenTooLarge is returned when a handle token exceeds the size limit.
var ErrHandleTokenTooLarge = errors.New("metry/async: async handle token too large")

// ErrNilTracer is returned when a linked span is started with a nil tracer.
var ErrNilTracer = errors.New("metry/async: tracer is nil")

type handlePayload struct {
	TraceID    string `json:"trace_id"`
	SpanID     string `json:"span_id"`
	TraceFlags byte   `json:"trace_flags"`
	TraceState string `json:"trace_state,omitempty"`
	Remote     bool   `json:"remote"`
}

// Handle is a serializable token linking deferred outcomes to an originating interaction.
type Handle struct {
	spanContext trace.SpanContext
}

// NewNoopHandle returns a valid synthetic handle for disabled telemetry paths.
func NewNoopHandle() Handle {
	return Handle{spanContext: trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14},
		SpanID:     trace.SpanID{15, 15, 15, 15, 15, 15, 15, 15},
		TraceFlags: 0,
		TraceState: trace.TraceState{},
		Remote:     true,
	})}
}

// NewHandle captures the current span context from ctx as a portable handle.
func NewHandle(ctx context.Context) (Handle, error) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return Handle{}, ErrNoSpanContext
	}
	return Handle{spanContext: sc}, nil
}

// newHandleFromSpanContext builds a handle when the span context is already known (tests and internal use).
func newHandleFromSpanContext(sc trace.SpanContext) (Handle, error) {
	if !sc.IsValid() {
		return Handle{}, ErrInvalidHandle
	}
	return Handle{spanContext: sc}, nil
}

// IsValid reports whether the handle references a valid span context.
func (h Handle) IsValid() bool {
	return h.spanContext.IsValid()
}

// Marshal encodes the handle as a portable string token.
func (h Handle) Marshal() (string, error) {
	if !h.spanContext.IsValid() {
		return "", ErrInvalidHandle
	}
	payload := handlePayload{
		TraceID:    h.spanContext.TraceID().String(),
		SpanID:     h.spanContext.SpanID().String(),
		TraceFlags: byte(h.spanContext.TraceFlags()),
		TraceState: h.spanContext.TraceState().String(),
		Remote:     h.spanContext.IsRemote(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("metry/async: marshal handle: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if len(token) > maxHandleTokenLen {
		return "", ErrHandleTokenTooLarge
	}
	return token, nil
}

// ParseHandle decodes a token produced by Handle.Marshal.
func ParseHandle(token string) (Handle, error) {
	if token == "" {
		return Handle{}, ErrInvalidHandle
	}
	if len(token) > maxHandleTokenLen {
		return Handle{}, ErrHandleTokenTooLarge
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Handle{}, ErrInvalidHandle
	}
	var payload handlePayload
	if unmarshalErr := json.Unmarshal(raw, &payload); unmarshalErr != nil {
		return Handle{}, ErrInvalidHandle
	}
	traceID, err := trace.TraceIDFromHex(payload.TraceID)
	if err != nil {
		return Handle{}, ErrInvalidHandle
	}
	spanID, err := trace.SpanIDFromHex(payload.SpanID)
	if err != nil {
		return Handle{}, ErrInvalidHandle
	}
	ts, err := trace.ParseTraceState(payload.TraceState)
	if err != nil && payload.TraceState != "" {
		return Handle{}, ErrInvalidHandle
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.TraceFlags(payload.TraceFlags),
		TraceState: ts,
		Remote:     payload.Remote,
	})
	if !sc.IsValid() {
		return Handle{}, ErrInvalidHandle
	}
	return Handle{spanContext: sc}, nil
}
