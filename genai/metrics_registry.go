package genai

import (
	"errors"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/metric"
)

type metricsHolder struct {
	InputTokens  metric.Int64Counter
	OutputTokens metric.Int64Counter
	Cost         metric.Float64Counter
	Ttft         metric.Float64Histogram
	Tps          metric.Float64Histogram
	Tbt          metric.Float64Histogram
}

var globalMetrics atomic.Pointer[metricsHolder]
var registerMetricsMu sync.Mutex

var errMetricsAlreadyRegistered = errors.New("genai: metrics already registered, call shutdown first")

// RegisterMetricsForInit registers GenAI metric instruments on a meter.
// It is intended for use by metry.Init; application code should continue using metry.Init.
func RegisterMetricsForInit(meter metric.Meter) (cleanup func(), err error) {
	if meter == nil {
		return nil, errors.New("genai: meter must not be nil")
	}
	registerMetricsMu.Lock()
	defer registerMetricsMu.Unlock()
	if globalMetrics.Load() != nil {
		return nil, errMetricsAlreadyRegistered
	}

	inTokens, err := meter.Int64Counter(InputTokensMetricName)
	if err != nil {
		return nil, err
	}
	outTokens, err := meter.Int64Counter(OutputTokensMetricName)
	if err != nil {
		return nil, err
	}
	cost, err := meter.Float64Counter(CostMetricName)
	if err != nil {
		return nil, err
	}
	ttft, err := meter.Float64Histogram(
		TTFTMetricName,
		metric.WithUnit("s"),
		metric.WithDescription("Time to first token in LLM streaming responses"),
	)
	if err != nil {
		return nil, err
	}
	tps, err := meter.Float64Histogram(
		StreamingTPSMetricName,
		metric.WithUnit("token/s"),
		metric.WithDescription("Tokens per second during the streaming generation window"),
	)
	if err != nil {
		return nil, err
	}
	tbt, err := meter.Float64Histogram(
		StreamingTBTMetricName,
		metric.WithUnit("s"),
		metric.WithDescription("Time between tokens during streaming generation"),
	)
	if err != nil {
		return nil, err
	}

	holder := &metricsHolder{
		InputTokens:  inTokens,
		OutputTokens: outTokens,
		Cost:         cost,
		Ttft:         ttft,
		Tps:          tps,
		Tbt:          tbt,
	}
	globalMetrics.Store(holder)
	return func() {
		globalMetrics.CompareAndSwap(holder, nil)
	}, nil
}

func currentMetricsHolder() *metricsHolder {
	return globalMetrics.Load()
}
