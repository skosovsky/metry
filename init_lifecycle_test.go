// Package metry_test (external test) allows importing testutil without import cycle.
package metry_test

import (
	"context"
	"testing"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/testutil"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const (
	ttftMetricName   = "gen_ai.client.ttft"
	inputTokensName  = "gen_ai.client.token.usage.input"  // #nosec G101 -- OTel metric name, not a credential
	outputTokensName = "gen_ai.client.token.usage.output" // #nosec G101 -- OTel metric name, not a credential
	costMetricName   = "gen_ai.client.cost"
)

// TestInit_Shutdown_Init_GenAIMetricsWork verifies that after Init -> shutdown -> Init,
// GenAI metrics are registered again, RecordTTFT/RecordUsage do not panic, and exported
// datapoints after the second Init are present (lifecycle + delivery).
func TestInit_Shutdown_Init_GenAIMetricsWork(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewInMemoryMetricExporter()
	tr := testutil.NewInMemoryTraceExporter()

	shutdown1, err := metry.Init(ctx,
		metry.WithServiceName("test-svc"),
		metry.WithTraceExporter(tr.TraceExporter()),
		metry.WithMetricExporter(*mem.MetricExporter()),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown1)
	genai.RecordTTFT(ctx, 0.1, "model-a")
	require.NoError(t, shutdown1(ctx))

	shutdown2, err := metry.Init(ctx,
		metry.WithServiceName("test-svc"),
		metry.WithTraceExporter(tr.TraceExporter()),
		metry.WithMetricExporter(*mem.MetricExporter()),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown2)

	// Second init: record and then shutdown to flush so we can assert on datapoints
	genai.RecordTTFT(ctx, 0.2, "model-b")
	_, span := metry.GlobalTracer().Start(ctx, "span")
	genai.RecordUsage(ctx, span, 1, 2, 0.001)
	span.End()

	require.NoError(t, shutdown2(ctx))
	rm := mem.LastResourceMetrics()
	require.NotNil(t, rm, "exporter must receive ResourceMetrics after shutdown flush")

	// TTFT: prove histogram export after second Init
	count, sum, model := getTTFTFromResourceMetrics(t, *rm)
	require.Equal(t, uint64(1), count, "TTFT histogram must have one datapoint after second Init")
	require.InDelta(t, 0.2, sum, 1e-9, "TTFT sum")
	require.Equal(t, "model-b", model, "TTFT model attribute")

	// Usage: prove counters (input, output, cost) are exported after second Init
	inputVal, outputVal, costVal := getUsageFromResourceMetrics(t, *rm)
	require.Equal(t, int64(1), inputVal, "input tokens counter after second Init")
	require.Equal(t, int64(2), outputVal, "output tokens counter after second Init")
	require.InDelta(t, 0.001, costVal, 1e-9, "cost counter after second Init")
}

// getTTFTFromResourceMetrics extracts TTFT histogram count, sum and model from ResourceMetrics.
func getTTFTFromResourceMetrics(t *testing.T, rm metricdata.ResourceMetrics) (count uint64, sum float64, model string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != ttftMetricName {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "metric %q should be Histogram[float64]", ttftMetricName)
			require.NotEmpty(t, hist.DataPoints, "TTFT should have at least one datapoint")
			dp := hist.DataPoints[0]
			count = dp.Count
			sum = dp.Sum
			if v, ok := dp.Attributes.Value(genai.RequestModelKey); ok {
				model = v.AsString()
			}
			return count, sum, model
		}
	}
	t.Fatalf("metric %q not found in ResourceMetrics", ttftMetricName)
	return 0, 0, ""
}

// getUsageFromResourceMetrics extracts input tokens, output tokens and cost sums from ResourceMetrics.
func getUsageFromResourceMetrics(t *testing.T, rm metricdata.ResourceMetrics) (input int64, output int64, cost float64) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case inputTokensName:
				input = getSumInt64FromMetrics(t, m)
			case outputTokensName:
				output = getSumInt64FromMetrics(t, m)
			case costMetricName:
				cost = getSumFloat64FromMetrics(t, m)
			}
		}
	}
	return input, output, cost
}

func getSumInt64FromMetrics(t *testing.T, m metricdata.Metrics) int64 {
	t.Helper()
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "metric %q should be Sum[int64]", m.Name)
	var total int64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}

func getSumFloat64FromMetrics(t *testing.T, m metricdata.Metrics) float64 {
	t.Helper()
	sum, ok := m.Data.(metricdata.Sum[float64])
	require.True(t, ok, "metric %q should be Sum[float64]", m.Name)
	var total float64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}
