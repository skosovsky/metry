// Package traceutil provides utilities for OpenTelemetry trace spans.
package traceutil

import (
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// SpanError records the error on the span and sets the span status to Error.
// If err is nil, it does nothing.
func SpanError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SpanOK sets the span status to Ok with an empty description.
func SpanOK(span trace.Span) {
	span.SetStatus(codes.Ok, "")
}

// MutateRecordingSpan runs fn only when span is recording.
func MutateRecordingSpan(span trace.Span, fn func(trace.Span)) {
	if span == nil || !span.IsRecording() {
		return
	}
	fn(span)
}

// SpanOKIfUnset sets Ok only when span status is still Unset.
func SpanOKIfUnset(span trace.Span) {
	if rs, ok := span.(interface {
		Status() sdktrace.Status
	}); ok && rs.Status().Code != codes.Unset {
		return
	}
	SpanOK(span)
}

// EndSpanOKIfUnset sets Ok when status is Unset, then ends the span.
func EndSpanOKIfUnset(span trace.Span) {
	MutateRecordingSpan(span, SpanOKIfUnset)
	span.End()
}
