package genai

import (
	"context"
	"testing"

	"github.com/skosovsky/metry/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestInit_NilMeter_ReturnsError(t *testing.T) {
	err := Init(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "meter must not be nil")
}

func TestInit_RecordUsage_IncrementsCounters(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	t.Cleanup(func() {
		inputTokensCounter = nil
		outputTokensCounter = nil
		costCounter = nil
		ttftHistogram = nil
	})
	meter := mp.Meter("genai-test")
	err := Init(meter)
	require.NoError(t, err)

	ctx := context.Background()
	span := noop.Span{}
	RecordUsage(ctx, &span, 10, 20, 0.001)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	inputVal := getSumInt64(t, rm, inputTokensCounterName)
	outputVal := getSumInt64(t, rm, outputTokensCounterName)
	costVal := getSumFloat64(t, rm, costCounterName)

	require.Equal(t, int64(10), inputVal, "input tokens counter")
	require.Equal(t, int64(20), outputVal, "output tokens counter")
	require.InDelta(t, 0.001, costVal, 1e-9, "cost counter")
}

func TestRecordTTFT_RecordsHistogram(t *testing.T) {
	reader, meter := testutil.SetupTestMetrics(t)
	t.Cleanup(func() {
		inputTokensCounter = nil
		outputTokensCounter = nil
		costCounter = nil
		ttftHistogram = nil
	})
	err := Init(meter)
	require.NoError(t, err)

	ctx := context.Background()
	RecordTTFT(ctx, 0.42)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	count, sum := getHistogramFloat64(t, rm, ttftHistogramName)
	require.Equal(t, uint64(1), count, "histogram count")
	require.InDelta(t, 0.42, sum, 1e-9, "histogram sum")
}

func getHistogramFloat64(t *testing.T, rm metricdata.ResourceMetrics, name string) (count uint64, sum float64) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "metric %q should be Histogram[float64]", name)
			require.NotEmpty(t, hist.DataPoints, "histogram should have at least one data point")
			dp := hist.DataPoints[0]
			return dp.Count, dp.Sum
		}
	}
	t.Fatalf("metric %q not found", name)
	return 0, 0
}

func getSumInt64(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "metric %q should be Sum[int64]", name)
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	t.Fatalf("metric %q not found", name)
	return 0
}

func getSumFloat64(t *testing.T, rm metricdata.ResourceMetrics, name string) float64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[float64])
			require.True(t, ok, "metric %q should be Sum[float64]", name)
			var total float64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	t.Fatalf("metric %q not found", name)
	return 0
}
