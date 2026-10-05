package metrytest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/testutil"
)

// AsyncHandleFromSpanContext builds an async handle from a synthetic span context.
func AsyncHandleFromSpanContext(t *testing.T, sc trace.SpanContext) metry.AsyncHandle {
	t.Helper()
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	handle, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	return handle
}

// AsyncHandleFromProducerSpan creates an async handle from a producer span and returns the handle with the flushed producer stub.
func AsyncHandleFromProducerSpan(
	t *testing.T,
	provider *metry.Provider,
	mem *testutil.InMemoryTraceExporter,
	component, spanName string,
) (metry.AsyncHandle, tracetest.SpanStub) {
	t.Helper()
	ctx, end, err := provider.StartSpan(context.Background(), component, spanName)
	require.NoError(t, err)
	handle, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	end()
	require.NoError(t, provider.ForceFlush(context.Background()))
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	return handle, spans[0]
}

// CollectResourceMetrics flushes the provider and returns the last exported resource metrics.
func CollectResourceMetrics(
	t *testing.T,
	provider *metry.Provider,
	mem *testutil.InMemoryMetricExporter,
) metricdata.ResourceMetrics {
	t.Helper()
	require.NoError(t, provider.ForceFlush(context.Background()))
	rm := mem.LastResourceMetrics()
	require.NotNil(t, rm)
	return *rm
}

// Float64HistogramSum returns the sum of all data points for a float64 histogram metric.
func Float64HistogramSum(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector) float64 {
	t.Helper()
	hist := FindFloat64Histogram(t, rm, selector)
	var total float64
	for _, dp := range hist.DataPoints {
		total += dp.Sum
	}
	return total
}

// Int64HistogramSumByAttr returns the sum of int64 histogram points matching an attribute value.
func Int64HistogramSumByAttr(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	selector MetricSelector,
	attrKey, attrValue string,
) float64 {
	t.Helper()
	hist := FindInt64Histogram(t, rm, selector)
	var total int64
	for _, dp := range hist.DataPoints {
		if testutil.SpanHasAttr(dp.Attributes, attrKey) &&
			testutil.SpanStringAttr(t, dp.Attributes, attrKey) == attrValue {
			total += dp.Sum
		}
	}
	return float64(total)
}

// Float64SumValue returns the total value of a float64 sum metric.
func Float64SumValue(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector) float64 {
	t.Helper()
	sum := FindFloat64Sum(t, rm, selector)
	var total float64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}

// FirstFloat64SumAttr returns the string value of an attribute on the first data point of a float64 sum metric.
func FirstFloat64SumAttr(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector, key string) string {
	t.Helper()
	sum := FindFloat64Sum(t, rm, selector)
	require.NotEmpty(t, sum.DataPoints)
	return testutil.SpanStringAttr(t, sum.DataPoints[0].Attributes, key)
}

// AssertMetricAbsent fails if a metric matching the selector exists.
func AssertMetricAbsent(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector) {
	t.Helper()
	validateSelector(t, selector)
	for _, sm := range rm.ScopeMetrics {
		if !matchesScope(sm.Scope, selector.Scope) {
			continue
		}
		for _, m := range sm.Metrics {
			if m.Name == selector.Name && (selector.Aggregation == 0 || aggregationOf(m.Data) == selector.Aggregation) {
				t.Fatalf("metric %q unexpectedly found", selector.Name)
			}
		}
	}
}

// FindInt64Histogram locates exactly one int64 histogram in the selected scope.
func FindInt64Histogram(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	selector MetricSelector,
) metricdata.Histogram[int64] {
	t.Helper()
	data, ok := selectMetric(t, rm, selector, Int64Histogram).Data.(metricdata.Histogram[int64])
	require.True(t, ok, "metric %q has wrong aggregation", selector.Name)
	return data
}

// FindFloat64Histogram locates exactly one float64 histogram in the selected scope.
func FindFloat64Histogram(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	selector MetricSelector,
) metricdata.Histogram[float64] {
	t.Helper()
	data, ok := selectMetric(t, rm, selector, Float64Histogram).Data.(metricdata.Histogram[float64])
	require.True(t, ok, "metric %q has wrong aggregation", selector.Name)
	return data
}

// FindFloat64Sum locates exactly one float64 sum in the selected scope.
func FindFloat64Sum(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector) metricdata.Sum[float64] {
	t.Helper()
	data, ok := selectMetric(t, rm, selector, Float64Sum).Data.(metricdata.Sum[float64])
	require.True(t, ok, "metric %q has wrong aggregation", selector.Name)
	return data
}

// FindInt64Sum locates exactly one int64 sum in the selected scope.
func FindInt64Sum(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector) metricdata.Sum[int64] {
	t.Helper()
	data, ok := selectMetric(t, rm, selector, Int64Sum).Data.(metricdata.Sum[int64])
	require.True(t, ok, "metric %q has wrong aggregation", selector.Name)
	return data
}

// Int64SumValue returns the total of all int64 sum data points (counter).
func Int64SumValue(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector) int64 {
	t.Helper()
	sum := FindInt64Sum(t, rm, selector)
	var total int64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}

// FirstInt64SumAttr returns a string attribute from the first counter data point.
func FirstInt64SumAttr(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector, key string) string {
	t.Helper()
	sum := FindInt64Sum(t, rm, selector)
	require.NotEmpty(t, sum.DataPoints, "metric %q has no data points", selector)
	return testutil.SpanStringAttr(t, sum.DataPoints[0].Attributes, key)
}

// FindGauge locates exactly one float64 gauge in the selected scope.
func FindGauge(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector) metricdata.Gauge[float64] {
	t.Helper()
	data, ok := selectMetric(t, rm, selector, Float64Gauge).Data.(metricdata.Gauge[float64])
	require.True(t, ok, "metric %q has wrong aggregation", selector.Name)
	return data
}

// GaugeFloat64Value returns the value of the first gauge data point.
func GaugeFloat64Value(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector) float64 {
	t.Helper()
	gauge := FindGauge(t, rm, selector)
	require.NotEmpty(t, gauge.DataPoints, "metric %q has no data points", selector)
	return gauge.DataPoints[0].Value
}

// FirstGaugeAttr returns a string attribute from the first gauge data point.
func FirstGaugeAttr(t *testing.T, rm metricdata.ResourceMetrics, selector MetricSelector, key string) string {
	t.Helper()
	gauge := FindGauge(t, rm, selector)
	require.NotEmpty(t, gauge.DataPoints)
	return testutil.SpanStringAttr(t, gauge.DataPoints[0].Attributes, key)
}
