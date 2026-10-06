// Package traceutil provides utilities for OpenTelemetry trace spans.
package traceutil

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/diagnostic"
)

// SpanError records the error on the span and sets the span status to Error.
// If err is nil, it does nothing.
func SpanError(span trace.Span, err error) {
	if err == nil {
		return
	}
	RecordDiagnostic(span, (diagnostic.Policy{}).Error(err))
}

// SpanErrorWithPolicy processes an error before recording any sink data.
func SpanErrorWithPolicy(span trace.Span, err error, policy diagnostic.Policy) {
	if err != nil {
		RecordDiagnostic(span, policy.Error(err))
	}
}

// RecordDiagnostic records a previously processed result without the original object.
func RecordDiagnostic(span trace.Span, result diagnostic.Result) {
	if span == nil || result.Class == "" {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("exception.type", result.Class),
		attribute.String("exception.message", result.Message),
	}
	if result.Stacktrace != "" {
		attrs = append(attrs, attribute.String("exception.stacktrace", result.Stacktrace))
	}
	span.AddEvent("exception", trace.WithAttributes(attrs...))
	span.SetStatus(codes.Error, result.Message)
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
