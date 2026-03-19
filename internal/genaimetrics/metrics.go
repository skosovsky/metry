// Package genaimetrics provides internal registration of GenAI metric instruments.
// Only metry.Init and tests under this module may import it; external code cannot (Go internal rule).
package genaimetrics

import (
	"errors"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/metric"
)

const (
	inputTokensCounterName  = "gen_ai.client.token.usage.input"  // #nosec G101 -- OTel metric name, not a credential
	outputTokensCounterName = "gen_ai.client.token.usage.output" // #nosec G101 -- OTel metric name, not a credential
	costCounterName         = "gen_ai.client.cost"
	ttftHistogramName       = "gen_ai.client.ttft"
	tpsHistogramName        = "gen_ai.streaming.tps"
	tbtHistogramName        = "gen_ai.streaming.tbt"
)

// MetricsHolder holds GenAI metric instruments. Used by genai package for RecordInteraction and streaming helpers.
type MetricsHolder struct {
	InputTokens  metric.Int64Counter
	OutputTokens metric.Int64Counter
	Cost         metric.Float64Counter
	Ttft         metric.Float64Histogram
	Tps          metric.Float64Histogram
	Tbt          metric.Float64Histogram
}

var globalMetrics atomic.Pointer[MetricsHolder]
var registerMu sync.Mutex

// ErrMetricsAlreadyRegistered is returned when RegisterMetrics is called while metrics are already
// registered (e.g. double Init without shutdown). Call shutdown before Init again.
var ErrMetricsAlreadyRegistered = errors.New("genai: metrics already registered, call shutdown first")

// RegisterMetrics registers GenAI metric instruments with the given meter. Called by metry.Init only.
// Lifecycle-safe: use registerMu (not sync.Once) so that after shutdown (cleanup), a second Init can
// register again. If called while already registered, returns ErrMetricsAlreadyRegistered; call shutdown first.
// Returns a cleanup that clears the holder via CompareAndSwap so the next Init can register again.
func RegisterMetrics(meter metric.Meter) (cleanup func(), err error) {
	if meter == nil {
		return nil, errors.New("genai: meter must not be nil")
	}
	registerMu.Lock()
	defer registerMu.Unlock()
	if globalMetrics.Load() != nil {
		return nil, ErrMetricsAlreadyRegistered
	}
	inTokens, err := meter.Int64Counter(inputTokensCounterName)
	if err != nil {
		return nil, err
	}
	outTokens, err := meter.Int64Counter(outputTokensCounterName)
	if err != nil {
		return nil, err
	}
	cost, err := meter.Float64Counter(costCounterName)
	if err != nil {
		return nil, err
	}
	ttft, err := meter.Float64Histogram(
		ttftHistogramName,
		metric.WithUnit("s"),
		metric.WithDescription("Time to first token in LLM streaming responses"),
	)
	if err != nil {
		return nil, err
	}
	tps, err := meter.Float64Histogram(
		tpsHistogramName,
		metric.WithUnit("token/s"),
		metric.WithDescription("Tokens per second during the streaming generation window"),
	)
	if err != nil {
		return nil, err
	}
	tbt, err := meter.Float64Histogram(
		tbtHistogramName,
		metric.WithUnit("s"),
		metric.WithDescription("Time between tokens during streaming generation"),
	)
	if err != nil {
		return nil, err
	}
	holder := &MetricsHolder{
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

// Holder returns the current metrics holder for hot-path reads from the genai package.
func Holder() *MetricsHolder {
	return globalMetrics.Load()
}
