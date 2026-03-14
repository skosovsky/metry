package genai

import (
	"context"
	"errors"
	"sync/atomic"

	"go.opentelemetry.io/otel/metric"
)

const (
	inputTokensCounterName  = "gen_ai.client.token.usage.input"  // #nosec G101 -- OTel metric name, not a credential
	outputTokensCounterName = "gen_ai.client.token.usage.output" // #nosec G101 -- OTel metric name, not a credential
	costCounterName         = "gen_ai.client.cost"
	ttftHistogramName       = "gen_ai.client.ttft"
)

// metricsHolder holds all GenAI metric instruments; access via globalMetrics after RegisterMetrics.
type metricsHolder struct {
	inputTokens  metric.Int64Counter
	outputTokens metric.Int64Counter
	cost         metric.Float64Counter
	ttft         metric.Float64Histogram
}

// globalMetrics is a lock-free pointer for hot-path reads (RecordTTFT, RecordUsageWithPurpose).
var globalMetrics atomic.Pointer[metricsHolder]

// RegisterMetrics registers GenAI metric instruments (token usage, cost, TTFT) with the given meter.
// It returns a cleanup function that clears the global holder so the next metry.Init can register
// on a new MeterProvider. Called by metry.Init; do not call from application code.
func RegisterMetrics(meter metric.Meter) (cleanup func(), err error) {
	if meter == nil {
		return func() {}, errors.New("genai: meter must not be nil")
	}
	inTokens, err := meter.Int64Counter(inputTokensCounterName)
	if err != nil {
		return func() {}, err
	}
	outTokens, err := meter.Int64Counter(outputTokensCounterName)
	if err != nil {
		return func() {}, err
	}
	cost, err := meter.Float64Counter(costCounterName)
	if err != nil {
		return func() {}, err
	}
	ttft, err := meter.Float64Histogram(
		ttftHistogramName,
		metric.WithUnit("s"),
		metric.WithDescription("Time to first token in LLM streaming responses"),
	)
	if err != nil {
		return func() {}, err
	}
	holder := &metricsHolder{
		inputTokens:  inTokens,
		outputTokens: outTokens,
		cost:         cost,
		ttft:         ttft,
	}
	globalMetrics.Store(holder)
	return func() {
		globalMetrics.CompareAndSwap(holder, nil)
	}, nil
}

// RecordTTFT records the Time To First Token (in seconds) as a histogram metric with model dimension.
// modelName is recorded as an attribute so dashboards can show TTFT per LLM (e.g. gpt-4o vs claude-3-5).
// Metrics are registered automatically when metry.Init is called with a metric exporter.
func RecordTTFT(ctx context.Context, durationSeconds float64, modelName string) {
	holder := globalMetrics.Load()
	if holder != nil && holder.ttft != nil {
		opts := metric.WithAttributes(RequestModelKey.String(modelName))
		holder.ttft.Record(ctx, durationSeconds, opts)
	}
}
