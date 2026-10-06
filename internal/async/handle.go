package async

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"
)

const maxHandleTokenLen = traceutil.MaxTokenBytes

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
	disabled    bool
}

// NewNoopHandle returns a disabled handle with no trace identity.
func NewNoopHandle() Handle { return Handle{spanContext: trace.SpanContext{}, disabled: true} }

// NewHandle captures the current span context from ctx as a portable handle.
func NewHandle(ctx context.Context) (Handle, error) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return Handle{}, ErrNoSpanContext
	}
	return Handle{spanContext: sc, disabled: false}, nil
}

// newHandleFromSpanContext builds a handle when the span context is already known (tests and internal use).
func newHandleFromSpanContext(sc trace.SpanContext) (Handle, error) {
	if !sc.IsValid() {
		return Handle{}, ErrInvalidHandle
	}
	return Handle{spanContext: sc, disabled: false}, nil
}

// IsDisabled distinguishes an explicit noop from an invalid zero handle internally.
func (h Handle) IsDisabled() bool { return h.disabled }

// IsValid reports whether the handle references a valid span context.
func (h Handle) IsValid() bool {
	return !h.disabled && h.spanContext.IsValid()
}

// SpanContext exposes the reference only to internal instrumentation adapters.
func (h Handle) SpanContext() trace.SpanContext {
	if h.disabled {
		return trace.SpanContext{}
	}
	return h.spanContext
}

// Marshal encodes the handle as a portable string token.
func (h Handle) Marshal() (string, error) {
	if !h.IsValid() {
		return "", ErrInvalidHandle
	}
	if len(h.spanContext.TraceState().String()) > traceutil.MaxTraceStateBytes {
		return "", ErrHandleTokenTooLarge
	}
	payload := handlePayload{
		TraceID:    h.spanContext.TraceID().String(),
		SpanID:     h.spanContext.SpanID().String(),
		TraceFlags: byte(h.spanContext.TraceFlags()),
		TraceState: h.spanContext.TraceState().String(),
		Remote:     h.spanContext.IsRemote(),
	}
	raw, err := traceutil.MarshalToken(payload)
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
	if len(payload.TraceState) > traceutil.MaxTraceStateBytes {
		return Handle{}, ErrHandleTokenTooLarge
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
	return Handle{spanContext: sc, disabled: false}, nil
}
