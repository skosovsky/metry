// Package traceutil provides utilities for OpenTelemetry trace spans.
package traceutil

import (
	"go.opentelemetry.io/otel/codes"
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
