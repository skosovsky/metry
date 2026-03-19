package genai

import (
	"context"

	"go.opentelemetry.io/otel/metric"
)

// RecordTTFT records the Time To First Token (in seconds) as a histogram metric with model dimension.
// modelName is recorded as an attribute so dashboards can show TTFT per LLM (e.g. gpt-4o vs claude-3-5).
// Metrics are registered automatically when metry.Init is called with a metric exporter.
func RecordTTFT(ctx context.Context, durationSeconds float64, modelName string) {
	holder := currentMetricsHolder()
	if holder != nil && holder.Ttft != nil {
		opts := metric.WithAttributes(RequestModelKey.String(modelName))
		holder.Ttft.Record(ctx, durationSeconds, opts)
	}
}

// RecordStreamingCompletion records aggregate streaming quality metrics for a completed generation.
func RecordStreamingCompletion(
	ctx context.Context,
	modelName string,
	outputTokens int,
	ttftSeconds float64,
	totalDurationSeconds float64,
) {
	holder := currentMetricsHolder()
	if holder == nil {
		return
	}

	generationWindow := totalDurationSeconds - ttftSeconds
	if generationWindow <= 0 {
		return
	}

	opts := metric.WithAttributes(RequestModelKey.String(modelName))
	if holder.Tps != nil && outputTokens > 0 {
		holder.Tps.Record(ctx, float64(outputTokens)/generationWindow, opts)
	}
	if holder.Tbt != nil && outputTokens > 1 {
		holder.Tbt.Record(ctx, generationWindow/float64(outputTokens-1), opts)
	}
}
