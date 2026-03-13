package genai

import (
	"context"
	"errors"
	"sync"

	"go.opentelemetry.io/otel/metric"
)

const (
	inputTokensCounterName  = "gen_ai.client.token.usage.input"  // #nosec G101 -- OTel metric name, not a credential
	outputTokensCounterName = "gen_ai.client.token.usage.output" // #nosec G101 -- OTel metric name, not a credential
	costCounterName         = "gen_ai.client.cost"
	ttftHistogramName       = "gen_ai.client.ttft"
)

var (
	inputTokensCounter  metric.Int64Counter
	outputTokensCounter metric.Int64Counter
	costCounter         metric.Float64Counter
	ttftHistogram       metric.Float64Histogram

	initMu sync.Mutex
)

// Init creates OTel counters for token usage and cost. Call once after metry.Init
// so that RecordUsage can increment metrics for dashboards (e.g. Grafana).
// If Init is not called, RecordUsage still sets span attributes but does not update metrics.
// This function is safe to call concurrently and is idempotent: initialization is global
// to the process, and only the first successful call with a non-nil meter registers the instruments.
func Init(meter metric.Meter) error {
	if meter == nil {
		return errors.New("genai: meter must not be nil")
	}

	initMu.Lock()
	defer initMu.Unlock()

	if inputTokensCounter != nil && outputTokensCounter != nil && costCounter != nil && ttftHistogram != nil {
		return nil // already initialized
	}

	inTokens, err := meter.Int64Counter(inputTokensCounterName)
	if err != nil {
		return err
	}
	outTokens, err := meter.Int64Counter(outputTokensCounterName)
	if err != nil {
		return err
	}
	cost, err := meter.Float64Counter(costCounterName)
	if err != nil {
		return err
	}
	ttft, err := meter.Float64Histogram(
		ttftHistogramName,
		metric.WithUnit("s"),
		metric.WithDescription("Time to first token in LLM streaming responses"),
	)
	if err != nil {
		return err
	}

	inputTokensCounter = inTokens
	outputTokensCounter = outTokens
	costCounter = cost
	ttftHistogram = ttft

	return nil
}

// RecordTTFT records the Time To First Token (in seconds) as a histogram metric.
// Requires genai.Init to be called first.
func RecordTTFT(ctx context.Context, durationSeconds float64) {
	if ttftHistogram != nil {
		ttftHistogram.Record(ctx, durationSeconds)
	}
}
