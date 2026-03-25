package genai

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// RecordTTFT records custom client-side time-to-first-token on the default tracker.
func RecordTTFT(ctx context.Context, meta GenAIMeta, duration time.Duration) {
	Default().RecordTTFT(ctx, meta, duration)
}

// RecordTTFT records custom client-side time-to-first-token on an explicit tracker.
func (t *Tracker) RecordTTFT(ctx context.Context, meta GenAIMeta, duration time.Duration) {
	if t.metrics == nil || t.metrics.TTFT == nil || duration <= 0 {
		return
	}
	attrs, ok := metricAttributesFromMeta(meta)
	if !ok {
		return
	}
	t.metrics.TTFT.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}

// RecordStreamingCompletion records custom streaming quality metrics on the default tracker.
func RecordStreamingCompletion(
	ctx context.Context,
	meta GenAIMeta,
	outputTokens int,
	ttft time.Duration,
	totalDuration time.Duration,
) {
	Default().RecordStreamingCompletion(ctx, meta, outputTokens, ttft, totalDuration)
}

// RecordStreamingCompletion records custom streaming quality metrics on an explicit tracker.
func (t *Tracker) RecordStreamingCompletion(
	ctx context.Context,
	meta GenAIMeta,
	outputTokens int,
	ttft time.Duration,
	totalDuration time.Duration,
) {
	if t.metrics == nil {
		return
	}

	generationWindow := totalDuration - ttft
	if generationWindow <= 0 {
		return
	}

	attrs, ok := metricAttributesFromMeta(meta)
	if !ok {
		return
	}

	if t.metrics.TPS != nil && outputTokens > 0 {
		t.metrics.TPS.Record(ctx, float64(outputTokens)/generationWindow.Seconds(), metric.WithAttributes(attrs...))
	}
	if t.metrics.TBT != nil && outputTokens > 1 {
		t.metrics.TBT.Record(ctx, generationWindow.Seconds()/float64(outputTokens-1), metric.WithAttributes(attrs...))
	}
}
