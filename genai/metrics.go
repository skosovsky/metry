package genai

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"go.opentelemetry.io/otel/metric"
)

// RecordTTFT records custom client-side time-to-first-token on an explicit tracker.
func (t *Tracker) RecordTTFT(ctx context.Context, meta Meta, duration time.Duration) {
	if t.metrics == nil || t.metrics.TTFT == nil || duration <= 0 {
		return
	}
	meta = mergeMetaFromScope(ctx, meta)
	attrs, ok := metricAttributesFromMeta(meta)
	if !ok {
		return
	}
	t.metrics.TTFT.Record(
		ctx,
		duration.Seconds(),
		metric.WithAttributes(t.boundedMetricAttributes(TTFTMetricName, attrs)...),
	)
}

// RecordStreamingCompletion records custom streaming quality metrics on an explicit tracker.
func (t *Tracker) RecordStreamingCompletion(
	ctx context.Context,
	event StreamingCompletion,
) error {
	if err := t.validateUsage(event.Usage); err != nil {
		return err
	}
	timing := StreamTimings{TTFT: event.TTFT, FirstTokenPresent: event.FirstTokenPresent}
	if err := validateStreamTimings(timing, event.TotalDuration); err != nil {
		return err
	}
	meta := mergeMetaFromScope(ctx, event.Meta)
	if err := validateOperationName(meta.Operation); err != nil {
		return err
	}
	meta.Duration = event.TotalDuration
	t.recordUsageMetrics(ctx, meta, event.Usage)
	t.recordOperationDuration(ctx, meta)
	t.recordStreamingQuality(ctx, meta, event.Usage.OutputTokens, timing, event.TotalDuration)
	return nil
}

func validateStreamTimings(timing StreamTimings, totalDuration time.Duration) error {
	if totalDuration < 0 || timing.TTFT < 0 || timing.TTFT > totalDuration ||
		(!timing.FirstTokenPresent && timing.TTFT != 0) {
		return invalidObservation("stream_timing", "invalid_interval")
	}
	return nil
}

func (t *Tracker) recordStreamingQuality(
	ctx context.Context,
	meta Meta,
	outputTokens Measurement[int],
	timing StreamTimings,
	totalDuration time.Duration,
) {
	if t.metrics == nil {
		return
	}

	attrs, ok := metricAttributesFromMeta(meta)
	if !ok {
		return
	}
	if !timing.FirstTokenPresent {
		return
	}
	if t.metrics.TTFT != nil {
		t.metrics.TTFT.Record(
			ctx,
			timing.TTFT.Seconds(),
			metric.WithAttributes(t.boundedMetricAttributes(TTFTMetricName, attrs)...),
		)
	}
	generationWindow := totalDuration - timing.TTFT
	if generationWindow <= 0 || !outputTokens.Present {
		return
	}
	attrs = appendAttribute(attrs, attribute.String(usageSourceKey, string(outputTokens.Source)))

	if t.metrics.TPS != nil {
		t.metrics.TPS.Record(
			ctx,
			float64(outputTokens.Value)/generationWindow.Seconds(),
			metric.WithAttributes(t.boundedMetricAttributes(StreamingTPSMetricName, attrs)...),
		)
	}
	if t.metrics.TBT != nil && outputTokens.Value > 1 {
		t.metrics.TBT.Record(
			ctx,
			generationWindow.Seconds()/float64(outputTokens.Value-1),
			metric.WithAttributes(t.boundedMetricAttributes(StreamingTBTMetricName, attrs)...),
		)
	}
}
