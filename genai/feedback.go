package genai

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	// ErrParentSpanContextRequired is returned when async feedback is recorded without a valid parent.
	ErrParentSpanContextRequired = errors.New("genai: valid parent span context is required")
)

// RecordAsyncFeedback records delayed user feedback with an explicit tracker.
func (t *Tracker) RecordAsyncFeedback(
	ctx context.Context,
	parent trace.SpanContext,
	score float64,
	feedbackText string,
) error {
	if !parent.IsValid() {
		return ErrParentSpanContextRequired
	}

	if parent.IsRemote() {
		ctx = trace.ContextWithRemoteSpanContext(ctx, parent)
	} else {
		ctx = trace.ContextWithSpanContext(ctx, parent)
	}

	_, span := t.tracer.Start(ctx, "user_feedback")
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
