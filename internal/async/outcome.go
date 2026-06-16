package async

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"
)

// RecordLinkedOutcome records a deferred outcome as a new root span linked to the handle origin.
func (h Handle) RecordLinkedOutcome(
	ctx context.Context,
	tracer trace.Tracer,
	spanName string,
	attrs []attribute.KeyValue,
) error {
	return h.RunLinkedSpan(ctx, tracer, spanName, func(span trace.Span) error {
		if len(attrs) == 0 {
			return nil
		}
		filtered := make([]attribute.KeyValue, 0, len(attrs))
		for _, attr := range attrs {
			if attr.Key == "" {
				continue
			}
			filtered = append(filtered, attr)
		}
		if len(filtered) > 0 {
			traceutil.MutateRecordingSpan(span, func(span trace.Span) {
				span.SetAttributes(filtered...)
			})
		}
		return nil
	})
}
