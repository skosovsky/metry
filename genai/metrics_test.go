package genai

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRecordInteraction_RecordsTokenHistogramAndCost(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("genai-test"), noop.NewTracerProvider().Tracer("genai-test"))
	require.NoError(t, err)

	span := noop.Span{}
	tracker.RecordInteraction(context.Background(), &span, testMeta(), Payload{}, Usage{
		InputTokens:  10,
		OutputTokens: 20,
		Cost:         0.001,
	})

	rm := collectMetrics(t, reader)
	assert.InDelta(t, 10, int64HistogramSumByTokenType(t, rm, TokenUsageMetricName, TokenTypeInput), 1e-9)
	assert.InDelta(t, 20, int64HistogramSumByTokenType(t, rm, TokenUsageMetricName, TokenTypeOutput), 1e-9)
	assert.InDelta(t, 0.001, float64SumValue(t, rm, CostMetricName), 1e-9)
	assert.Equal(t, "USD", firstFloat64SumAttr(t, rm, CostMetricName, CostCurrencyKey))
}

func TestRecordInteraction_NegativeCost_IsIgnored(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("genai-test"), noop.NewTracerProvider().Tracer("genai-test"))
	require.NoError(t, err)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	tracker.RecordInteraction(context.Background(), rec, testMeta(), Payload{}, Usage{
		InputTokens:  10,
		OutputTokens: 20,
		Cost:         -0.25,
		Currency:     "USD",
	})

	_, ok := rec.attrs[UsageCostKey]
	assert.False(t, ok)
	rm := collectMetrics(t, reader)
	assertMetricAbsent(t, rm, CostMetricName)
}

func TestRecordTTFT_AndStreamingCompletion_RecordMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("genai-test"), noop.NewTracerProvider().Tracer("genai-test"))
	require.NoError(t, err)

	tracker.RecordTTFT(context.Background(), testMeta(), 420*time.Millisecond)
	tracker.RecordStreamingCompletion(context.Background(), testMeta(), 11, time.Second, 6*time.Second)

	rm := collectMetrics(t, reader)
	assert.InDelta(t, 0.42, float64HistogramSum(t, rm, TTFTMetricName), 1e-9)
	assert.InDelta(t, 2.2, float64HistogramSum(t, rm, StreamingTPSMetricName), 1e-9)
	assert.InDelta(t, 0.5, float64HistogramSum(t, rm, StreamingTBTMetricName), 1e-9)
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

func int64HistogramSumByTokenType(t *testing.T, rm metricdata.ResourceMetrics, name, tokenType string) float64 {
	t.Helper()
	hist := findInt64Histogram(t, rm, name)
	var total int64
	for _, dp := range hist.DataPoints {
		if v, ok := dp.Attributes.Value(TokenTypeKey); ok && v.AsString() == tokenType {
			total += dp.Sum
		}
	}
	return float64(total)
}

func float64HistogramSum(t *testing.T, rm metricdata.ResourceMetrics, name string) float64 {
	t.Helper()
	hist := findFloat64Histogram(t, rm, name)
	var total float64
	for _, dp := range hist.DataPoints {
		total += dp.Sum
	}
	return total
}

func float64SumValue(t *testing.T, rm metricdata.ResourceMetrics, name string) float64 {
	t.Helper()
	sum := findFloat64Sum(t, rm, name)
	var total float64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}

func firstFloat64SumAttr(t *testing.T, rm metricdata.ResourceMetrics, name string, key attribute.Key) string {
	t.Helper()
	sum := findFloat64Sum(t, rm, name)
	require.NotEmpty(t, sum.DataPoints)
	value, ok := sum.DataPoints[0].Attributes.Value(key)
	require.True(t, ok)
	return value.AsString()
}

func assertMetricAbsent(t *testing.T, rm metricdata.ResourceMetrics, name string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				t.Fatalf("metric %q unexpectedly found", name)
			}
		}
	}
}

func findInt64Histogram(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Histogram[int64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[int64])
			require.True(t, ok)
			return hist
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Histogram[int64]{}
}

func findFloat64Histogram(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Histogram[float64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			return hist
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Histogram[float64]{}
}

func findFloat64Sum(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Sum[float64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[float64])
			require.True(t, ok)
			return sum
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Sum[float64]{}
}

type recordingSpan struct {
	noop.Span

	attrs map[attribute.Key]attribute.Value
}

func (r *recordingSpan) SetAttributes(kv ...attribute.KeyValue) {
	if r.attrs == nil {
		r.attrs = make(map[attribute.Key]attribute.Value)
	}
	for _, item := range kv {
		r.attrs[item.Key] = item.Value
	}
}
