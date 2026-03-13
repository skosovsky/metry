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

func TestInit_IsIdempotentAndThreadSafe(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	t.Cleanup(func() {
		inputTokensCounter = nil
		outputTokensCounter = nil
		costCounter = nil
		ttftHistogram = nil
	})
	meter := mp.Meter("genai-test-idempotent")

	// Call Init multiple times to verify idempotency
	err1 := Init(meter)
	require.NoError(t, err1)

	err2 := Init(meter)
	require.NoError(t, err2)

	// Ensure metrics are recorded properly after multiple inits
	ctx := context.Background()
	RecordTTFT(ctx, 1.5)

	var rm metricdata.ResourceMetrics
	err := reader.Collect(ctx, &rm)
	require.NoError(t, err)

	ttftCount, _ := getHistogramFloat64(t, rm, ttftHistogramName)
	assert.Equal(t, uint64(1), ttftCount, "Histogram should be recorded successfully after multiple inits")
}

func TestInit_ConcurrentCalls(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	t.Cleanup(func() {
		inputTokensCounter = nil
		outputTokensCounter = nil
		costCounter = nil
		ttftHistogram = nil
	})
	meter := mp.Meter("genai-test-concurrent")

	// Call Init concurrently from multiple goroutines
	const numGoroutines = 10
	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			errCh <- Init(meter)
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		err := <-errCh
		require.NoError(t, err)
	}

	// Verify it actually initialized
	assert.NotNil(t, inputTokensCounter)
	assert.NotNil(t, outputTokensCounter)
	assert.NotNil(t, costCounter)
	assert.NotNil(t, ttftHistogram)
}

func TestInit_PartialInitializationDoesNotCorruptGlobals(t *testing.T) {
	// Setup partial failure condition if possible (though OpenTelemetry meter normally doesn't fail).
	// But we can verify that failing with nil meter doesn't clear already initialized globals.
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	t.Cleanup(func() {
		inputTokensCounter = nil
		outputTokensCounter = nil
		costCounter = nil
		ttftHistogram = nil
	})
	meter := mp.Meter("genai-test-partial")

	// Successful init
	err := Init(meter)
	require.NoError(t, err)

	// Verify globals are set
	assert.NotNil(t, inputTokensCounter)
	assert.NotNil(t, outputTokensCounter)

	// Attempt bad init
	err = Init(nil)
	require.Error(t, err)

	// Verify globals are STILL set and not overwritten with nil/bad state
	assert.NotNil(t, inputTokensCounter)
	assert.NotNil(t, outputTokensCounter)
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

func TestRecordUsageWithPurpose_RecordsMetricsWithPurposeAttribute(t *testing.T) {
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
	span := noop.Span{}

	RecordUsageWithPurpose(ctx, &span, 3, 7, 0.001, PurposeGuardEvaluation)
	RecordUsageWithPurpose(ctx, &span, 10, 20, 0.002, PurposeGeneration)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Total across all purposes (existing helpers) still matches sum of both calls
	inputVal := getSumInt64(t, rm, inputTokensCounterName)
	outputVal := getSumInt64(t, rm, outputTokensCounterName)
	costVal := getSumFloat64(t, rm, costCounterName)
	require.Equal(t, int64(13), inputVal, "input tokens total")
	require.Equal(t, int64(27), outputVal, "output tokens total")
	require.InDelta(t, 0.003, costVal, 1e-9, "cost total")

	// Datapoints are split by purpose so we can bill separately (input, output, cost)
	inputGuard := getSumInt64ByPurpose(t, rm, inputTokensCounterName, PurposeGuardEvaluation)
	require.Equal(t, int64(3), inputGuard, "guard_evaluation input tokens")
	inputGen := getSumInt64ByPurpose(t, rm, inputTokensCounterName, PurposeGeneration)
	require.Equal(t, int64(10), inputGen, "generation input tokens")
	outputGuard := getSumInt64ByPurpose(t, rm, outputTokensCounterName, PurposeGuardEvaluation)
	require.Equal(t, int64(7), outputGuard, "guard_evaluation output tokens")
	outputGen := getSumInt64ByPurpose(t, rm, outputTokensCounterName, PurposeGeneration)
	require.Equal(t, int64(20), outputGen, "generation output tokens")
	costGuard := getSumFloat64ByPurpose(t, rm, costCounterName, PurposeGuardEvaluation)
	require.InDelta(t, 0.001, costGuard, 1e-9, "guard_evaluation cost")
	costGen := getSumFloat64ByPurpose(t, rm, costCounterName, PurposeGeneration)
	require.InDelta(t, 0.002, costGen, 1e-9, "generation cost")
}

func TestRecordUsageWithPurpose_EmptyPurpose_AggregatesAsGeneration(t *testing.T) {
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
	span := noop.Span{}
	RecordUsageWithPurpose(ctx, &span, 1, 2, 0.001, "")

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Empty purpose normalizes to generation, so datapoints should appear under PurposeGeneration
	inputGen := getSumInt64ByPurpose(t, rm, inputTokensCounterName, PurposeGeneration)
	require.Equal(t, int64(1), inputGen, "empty purpose aggregates as generation input")
	outputGen := getSumInt64ByPurpose(t, rm, outputTokensCounterName, PurposeGeneration)
	require.Equal(t, int64(2), outputGen, "empty purpose aggregates as generation output")
	costGen := getSumFloat64ByPurpose(t, rm, costCounterName, PurposeGeneration)
	require.InDelta(t, 0.001, costGen, 1e-9, "empty purpose aggregates as generation cost")
}

func getSumInt64ByPurpose(t *testing.T, rm metricdata.ResourceMetrics, name, purpose string) int64 {
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
				if v, ok := dp.Attributes.Value(OperationPurposeKey); ok && v.AsString() == purpose {
					total += dp.Value
				}
			}
			return total
		}
	}
	t.Fatalf("metric %q not found", name)
	return 0
}

func getSumFloat64ByPurpose(t *testing.T, rm metricdata.ResourceMetrics, name, purpose string) float64 {
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
				if v, ok := dp.Attributes.Value(OperationPurposeKey); ok && v.AsString() == purpose {
					total += dp.Value
				}
			}
			return total
		}
	}
	t.Fatalf("metric %q not found", name)
	return 0
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
