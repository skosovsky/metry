package genai

import (
	"context"
	"crypto/rand"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RecordAsyncFeedback records delayed user feedback on the default tracker.
func RecordAsyncFeedback(
	ctx context.Context,
	traceIDHex string,
	score float64,
	feedbackText string,
) error {
	return Default().RecordAsyncFeedback(ctx, traceIDHex, score, feedbackText)
}

// RecordAsyncFeedback records delayed user feedback with an explicit tracker.
func (t *Tracker) RecordAsyncFeedback(
	ctx context.Context,
	traceIDHex string,
	score float64,
	feedbackText string,
) error {
	traceID, err := trace.TraceIDFromHex(traceIDHex)
	if err != nil {
		return err
	}
	spanID, err := randomSpanID()
	if err != nil {
		return err
	}

	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		TraceState: trace.TraceState{},
		Remote:     true,
	})
	ctx = trace.ContextWithRemoteSpanContext(ctx, parent)

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

func randomSpanID() (trace.SpanID, error) {
	for {
		var spanID trace.SpanID
		if _, err := rand.Read(spanID[:]); err != nil {
			return trace.SpanID{}, err
		}
		if spanID.IsValid() {
			return spanID, nil
		}
	}
}
