package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/skosovsky/metry/internal/genaimetrics"
)

const (
	ttftHistogramName       = "gen_ai.client.ttft"
	tpsHistogramName        = "gen_ai.streaming.tps"
	tbtHistogramName        = "gen_ai.streaming.tbt"
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
	assert.Equal(t, uint64(1), ttftCount)
}

func TestRegisterMetrics_LifecycleAfterCleanup(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test-lifecycle")

	cleanup1, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	cleanup1()
	require.Nil(t, genaimetrics.Holder())

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

	cleanupA()
	require.NotNil(t, genaimetrics.Holder())

	ctx := context.Background()
	RecordTTFT(ctx, 0.5, "model-b")
	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)
	count, sum := getHistogramFloat64(t, rm, ttftHistogramName)
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 0.5, sum, 1e-9)
}

func TestRecordInteraction_AfterRegisterMetrics_IncrementsCounters(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx := context.Background()
	span := noop.Span{}
	RecordInteraction(ctx, &span, GenAIPayload{}, GenAIUsage{
		InputTokens:  10,
		OutputTokens: 20,
		CostUSD:      0.001,
	})

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	inputVal := getSumInt64(t, rm, inputTokensCounterName)
	outputVal := getSumInt64(t, rm, outputTokensCounterName)
	costVal := getSumFloat64(t, rm, costCounterName)

	require.Equal(t, int64(10), inputVal)
	require.Equal(t, int64(20), outputVal)
	require.InDelta(t, 0.001, costVal, 1e-9)
}

func TestRecordInteraction_RecordsMetricsWithPurposeAttribute(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx := context.Background()
	span := noop.Span{}

	RecordInteraction(ctx, &span, GenAIPayload{}, GenAIUsage{
		InputTokens:  3,
		OutputTokens: 7,
		CostUSD:      0.001,
		Purpose:      PurposeGuardEvaluation,
	})
	RecordInteraction(ctx, &span, GenAIPayload{}, GenAIUsage{
		InputTokens:  10,
		OutputTokens: 20,
		CostUSD:      0.002,
		Purpose:      PurposeGeneration,
	})

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	require.Equal(t, int64(13), getSumInt64(t, rm, inputTokensCounterName))
	require.Equal(t, int64(27), getSumInt64(t, rm, outputTokensCounterName))
	require.InDelta(t, 0.003, getSumFloat64(t, rm, costCounterName), 1e-9)

	require.Equal(t, int64(3), getSumInt64ByPurpose(t, rm, inputTokensCounterName, PurposeGuardEvaluation))
	require.Equal(t, int64(10), getSumInt64ByPurpose(t, rm, inputTokensCounterName, PurposeGeneration))
	require.Equal(t, int64(7), getSumInt64ByPurpose(t, rm, outputTokensCounterName, PurposeGuardEvaluation))
	require.Equal(t, int64(20), getSumInt64ByPurpose(t, rm, outputTokensCounterName, PurposeGeneration))
	require.InDelta(t, 0.001, getSumFloat64ByPurpose(t, rm, costCounterName, PurposeGuardEvaluation), 1e-9)
	require.InDelta(t, 0.002, getSumFloat64ByPurpose(t, rm, costCounterName, PurposeGeneration), 1e-9)
}

func TestRecordInteraction_EmptyPurpose_AggregatesAsGeneration(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx := context.Background()
	span := noop.Span{}
	RecordInteraction(ctx, &span, GenAIPayload{}, GenAIUsage{
		InputTokens:  1,
		OutputTokens: 2,
		CostUSD:      0.001,
	})

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	require.Equal(t, int64(1), getSumInt64ByPurpose(t, rm, inputTokensCounterName, PurposeGeneration))
	require.Equal(t, int64(2), getSumInt64ByPurpose(t, rm, outputTokensCounterName, PurposeGeneration))
	require.InDelta(t, 0.001, getSumFloat64ByPurpose(t, rm, costCounterName, PurposeGeneration), 1e-9)
}

func TestRecordInteraction_ZeroUsage_DoesNotRecordCounters(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx := context.Background()
	span := noop.Span{}
	RecordInteraction(ctx, &span, GenAIPayload{}, GenAIUsage{})

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	assert.Equal(t, int64(0), getSumInt64IfPresent(rm, inputTokensCounterName))
	assert.Equal(t, int64(0), getSumInt64IfPresent(rm, outputTokensCounterName))
	assert.InDelta(t, 0.0, getSumFloat64IfPresent(rm, costCounterName), 1e-9)
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
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 0.42, sum, 1e-9)

	modelVal, ok := getHistogramDatapointModel(t, rm, ttftHistogramName)
	require.True(t, ok)
	assert.Equal(t, testModel, modelVal)
}

func TestRecordStreamingCompletion_RecordsHistograms(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx := context.Background()
	RecordStreamingCompletion(ctx, "gpt-4o", 11, 1.0, 6.0)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	tpsCount, tpsSum := getHistogramFloat64(t, rm, tpsHistogramName)
	require.Equal(t, uint64(1), tpsCount)
	require.InDelta(t, 2.2, tpsSum, 1e-9)

	tbtCount, tbtSum := getHistogramFloat64(t, rm, tbtHistogramName)
	require.Equal(t, uint64(1), tbtCount)
	require.InDelta(t, 0.5, tbtSum, 1e-9)

	modelVal, ok := getHistogramDatapointModel(t, rm, tpsHistogramName)
	require.True(t, ok)
	assert.Equal(t, "gpt-4o", modelVal)
}

func TestRecordStreamingCompletion_SkipsInvalidWindow(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx := context.Background()
	RecordStreamingCompletion(ctx, "model-a", 10, 2.0, 2.0)
	RecordStreamingCompletion(ctx, "model-a", 10, 3.0, 2.0)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	assert.Equal(t, uint64(0), getHistogramCountIfPresent(rm, tpsHistogramName))
	assert.Equal(t, uint64(0), getHistogramCountIfPresent(rm, tbtHistogramName))
}

func TestRecordStreamingCompletion_SkipsTBTForSingleToken(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test")
	cleanup, err := genaimetrics.RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx := context.Background()
	RecordStreamingCompletion(ctx, "model-a", 1, 0.5, 1.5)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	require.Equal(t, uint64(1), getHistogramCountIfPresent(rm, tpsHistogramName))
	require.Equal(t, uint64(0), getHistogramCountIfPresent(rm, tbtHistogramName))
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

func getHistogramFloat64(t *testing.T, rm metricdata.ResourceMetrics, name string) (count uint64, sum float64) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "metric %q should be Histogram[float64]", name)
			require.NotEmpty(t, hist.DataPoints)
			dp := hist.DataPoints[0]
			return dp.Count, dp.Sum
		}
	}
	t.Fatalf("metric %q not found", name)
	return 0, 0
}

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

func getHistogramCountIfPresent(rm metricdata.ResourceMetrics, name string) uint64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || len(hist.DataPoints) == 0 {
				return 0
			}
			return hist.DataPoints[0].Count
		}
	}
	return 0
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

func getSumInt64IfPresent(rm metricdata.ResourceMetrics, name string) int64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				return 0
			}
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	return 0
}

func getSumFloat64IfPresent(rm metricdata.ResourceMetrics, name string) float64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[float64])
			if !ok {
				return 0
			}
			var total float64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	return 0
}
