package metry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const maxTraceSnapshotTokenLen = 2048

var (
	// ErrNoTraceSnapshot is returned when no valid trace context can be captured.
	ErrNoTraceSnapshot = errors.New("metry: no valid trace context")
	// ErrInvalidTraceSnapshot is returned when a trace snapshot token cannot be parsed.
	ErrInvalidTraceSnapshot = errors.New("metry: invalid trace snapshot")
	// ErrTraceSnapshotTokenTooLarge is returned when a trace snapshot token exceeds the size limit.
	ErrTraceSnapshotTokenTooLarge = errors.New("metry: trace snapshot token too large")
)

// TraceSnapshot is an opaque durable token for restoring trace continuation.
type TraceSnapshot struct {
	traceParent string
	traceState  string
}

type traceSnapshotPayload struct {
	TraceParent string `json:"traceparent"`
	TraceState  string `json:"tracestate,omitempty"`
}

// TraceSnapshotFromContext captures trace continuation from ctx.
func TraceSnapshotFromContext(ctx context.Context) (TraceSnapshot, error) {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return TraceSnapshot{}, ErrNoTraceSnapshot
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return newTraceSnapshot(carrier.Get("traceparent"), carrier.Get("tracestate"))
}

// ParseTraceSnapshot decodes a token produced by TraceSnapshot.Marshal.
func ParseTraceSnapshot(token string) (TraceSnapshot, error) {
	if token == "" {
		return TraceSnapshot{}, ErrInvalidTraceSnapshot
	}
	if len(token) > maxTraceSnapshotTokenLen {
		return TraceSnapshot{}, ErrTraceSnapshotTokenTooLarge
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return TraceSnapshot{}, ErrInvalidTraceSnapshot
	}
	var payload traceSnapshotPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return TraceSnapshot{}, ErrInvalidTraceSnapshot
	}
	return newTraceSnapshot(payload.TraceParent, payload.TraceState)
}

// Marshal encodes the snapshot as an opaque portable token.
func (s TraceSnapshot) Marshal() (string, error) {
	if !s.IsValid() {
		return "", ErrInvalidTraceSnapshot
	}
	raw, err := json.Marshal(traceSnapshotPayload{
		TraceParent: s.traceParent,
		TraceState:  s.traceState,
	})
	if err != nil {
		return "", fmt.Errorf("metry: marshal trace snapshot: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if len(token) > maxTraceSnapshotTokenLen {
		return "", ErrTraceSnapshotTokenTooLarge
	}
	return token, nil
}

// IsValid reports whether the snapshot can restore a valid trace context.
func (s TraceSnapshot) IsValid() bool {
	if s.traceParent == "" {
		return false
	}
	ctx := propagation.TraceContext{}.Extract(context.Background(), snapshotCarrier(s))
	return trace.SpanContextFromContext(ctx).IsValid()
}

// ContextWithTraceSnapshot restores trace continuation from snapshot.
func (p *Provider) ContextWithTraceSnapshot(ctx context.Context, snapshot TraceSnapshot) (context.Context, error) {
	if !snapshot.IsValid() {
		return ctx, ErrInvalidTraceSnapshot
	}
	propagator := propagation.TextMapPropagator(propagation.TraceContext{})
	if p != nil {
		propagator = p.textMapPropagator()
	}
	return propagator.Extract(ctx, snapshotCarrier(snapshot)), nil
}

func newTraceSnapshot(traceParent, traceState string) (TraceSnapshot, error) {
	snapshot := TraceSnapshot{
		traceParent: traceParent,
		traceState:  traceState,
	}
	if !snapshot.IsValid() {
		return TraceSnapshot{}, ErrInvalidTraceSnapshot
	}
	return snapshot, nil
}

func snapshotCarrier(snapshot TraceSnapshot) propagation.MapCarrier {
	carrier := propagation.MapCarrier{"traceparent": snapshot.traceParent}
	if snapshot.traceState != "" {
		carrier["tracestate"] = snapshot.traceState
	}
	return carrier
}
