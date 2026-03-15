package genai

import (
	"context"

	"github.com/skosovsky/metry/internal/genaimetrics"
	"go.opentelemetry.io/otel/metric"
)

// RecordTTFT records the Time To First Token (in seconds) as a histogram metric with model dimension.
// modelName is recorded as an attribute so dashboards can show TTFT per LLM (e.g. gpt-4o vs claude-3-5).
// Metrics are registered automatically when metry.Init is called with a metric exporter.
func RecordTTFT(ctx context.Context, durationSeconds float64, modelName string) {
	holder := genaimetrics.Holder()
	if holder != nil && holder.Ttft != nil {
		opts := metric.WithAttributes(RequestModelKey.String(modelName))
		holder.Ttft.Record(ctx, durationSeconds, opts)
	}
}
