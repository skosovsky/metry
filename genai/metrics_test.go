package genai

import (
	"context"
	"testing"

	"github.com/skosovsky/metry/internal/genaimetrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

// Metric names for tests (must match internal/genaimetrics).
// "token" below refers to LLM tokens, not auth credentials (gosec G101 false positive).
const (
	ttftHistogramName       = "gen_ai.client.ttft"
	inputTokensCounterName  = "gen_ai.client.token.usage.input"  // #nosec G101
	outputTokensCounterName = "gen_ai.client.token.usage.output" // #nosec G101
	costCounterName         = "gen_ai.client.cost"
)

func TestRegisterMetrics_AfterCleanup_CanRegisterAgain(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test-lifecycle")

	cleanup1, err1 := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err1)
	cleanup1()

	cleanup2, err2 := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err2)
	t.Cleanup(cleanup2)

	ctx := context.Background()
	RecordTTFT(ctx, 1.5, "test-model")
	var rm metricdata.ResourceMetrics
	err := reader.Collect(ctx, &rm)
	require.NoError(t, err)
	ttftCount, _ := getHistogramFloat64(t, rm, ttftHistogramName)
	assert.Equal(t, uint64(1), ttftCount, "metrics work after cleanup and re-register")
}

// TestRegisterMetrics_LifecycleAfterCleanup verifies that after cleanup(), a new RegisterMetrics
// registers a fresh holder and metrics are recorded again (Init -> shutdown -> Init scenario).
func TestRegisterMetrics_LifecycleAfterCleanup(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test-lifecycle")

	cleanup1, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	cleanup1()
	require.Nil(t, genaimetrics.Holder(), "after cleanup Holder should be nil")

	cleanup2, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup2)

	ctx := context.Background()
	RecordTTFT(ctx, 0.1, "model-v2")
	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)
	count, sum := getHistogramFloat64(t, rm, ttftHistogramName)
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 0.1, sum, 1e-9)
}

// TestRegisterMetrics_CleanupOwnerSafe_DoesNotClearNewHolder verifies that calling the first
// cleanup after a second RegisterMetrics does not clear the new holder (CAS semantics).
func TestRegisterMetrics_CleanupOwnerSafe_DoesNotClearNewHolder(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test-owner-safe")

	cleanupA, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	cleanupA()
	cleanupB, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanupB)

	// Calling old cleanupA again must not clear B's holder (CompareAndSwap(holderA, nil) fails)
	cleanupA()
	require.NotNil(t, genaimetrics.Holder(), "cleanup from first holder must not clear second holder")

	// Recording must still work (B's holder is active)
	ctx := context.Background()
	RecordTTFT(ctx, 0.5, "model-b")
	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)
	count, sum := getHistogramFloat64(t, rm, ttftHistogramName)
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 0.5, sum, 1e-9)
}

func TestRecordUsage_AfterRegisterMetrics_IncrementsCounters(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

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
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

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
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

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
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx := context.Background()
	const testModel = "gpt-4o"
	RecordTTFT(ctx, 0.42, testModel)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	count, sum := getHistogramFloat64(t, rm, ttftHistogramName)
	require.Equal(t, uint64(1), count, "histogram count")
	require.InDelta(t, 0.42, sum, 1e-9, "histogram sum")

	// DoD: modelName must reach the exporter as gen_ai.request.model attribute.
	modelVal, ok := getHistogramDatapointModel(t, rm, ttftHistogramName)
	require.True(t, ok, "TTFT datapoint must have %s attribute", RequestModelKey)
	assert.Equal(t, testModel, modelVal, "TTFT recorded with wrong model label")
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

// getHistogramDatapointModel returns the gen_ai.request.model attribute from the first datapoint of the named histogram.
func getHistogramDatapointModel(t *testing.T, rm metricdata.ResourceMetrics, name string) (model string, ok bool) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || len(hist.DataPoints) == 0 {
				return "", false
			}
			v, ok := hist.DataPoints[0].Attributes.Value(RequestModelKey)
			if !ok {
				return "", false
			}
			return v.AsString(), true
		}
	}
	t.Fatalf("metric %q not found", name)
	return "", false
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
