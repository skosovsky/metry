package genai

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RecordAsyncFeedback records delayed user feedback as a new span linked to the original interaction.
// The original span is never mutated; correlation is expressed via Span Links (OTLP immutable model).
func (t *Tracker) RecordAsyncFeedback(
	ctx context.Context,
	linked trace.SpanContext,
	score float64,
	feedbackText string,
	opts ...trace.SpanStartOption,
) error {
	if !linked.IsValid() {
		return ErrInvalidSpanContext
	}

	startOpts := []trace.SpanStartOption{
		trace.WithNewRoot(),
		trace.WithLinks(trace.Link{SpanContext: linked, Attributes: nil}),
	}
	startOpts = append(startOpts, opts...)

	_, span := t.tracer.Start(ctx, "user_feedback", startOpts...)
	defer span.End()

	attrs := []attribute.KeyValue{
		EvaluationScoreKey.Float64(score),
	}
	if t.cfg.RecordPayloads() && feedbackText != "" {
		attrs = append(attrs, EvaluationTextKey.String(truncateContextWithConfig(feedbackText, t.cfg)))
	}
	span.SetAttributes(attrs...)
	return nil
}
