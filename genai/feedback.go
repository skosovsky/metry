package genai

import (
	"context"
	"crypto/rand"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RecordAsyncFeedback attaches delayed evaluation data to an existing trace.
func RecordAsyncFeedback(
	ctx context.Context,
	tracer trace.Tracer,
	traceIDHex string,
	score float64,
	feedbackText string,
) error {
	if tracer == nil {
		return errors.New("genai: tracer must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

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
		Remote:     true,
	})
	ctx = trace.ContextWithRemoteSpanContext(ctx, parent)

	_, span := tracer.Start(ctx, "user_feedback")
	defer span.End()

	cfg := currentConfig()
	attrs := []attribute.KeyValue{
		EvaluationScoreKey.Float64(score),
	}
	if cfg.RecordPayloads() && feedbackText != "" {
		attrs = append(attrs, EvaluationTextKey.String(truncateContextWithConfig(feedbackText, cfg)))
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
