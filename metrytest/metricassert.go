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
func Float64HistogramSum(t *testing.T, rm metricdata.ResourceMetrics, name string) float64 {
	t.Helper()
	hist := FindFloat64Histogram(t, rm, name)
	var total float64
	for _, dp := range hist.DataPoints {
		total += dp.Sum
	}
	return total
}

// Int64HistogramSumByAttr returns the sum of int64 histogram points matching an attribute value.
func Int64HistogramSumByAttr(t *testing.T, rm metricdata.ResourceMetrics, name, attrKey, attrValue string) float64 {
	t.Helper()
	hist := FindInt64Histogram(t, rm, name)
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
func Float64SumValue(t *testing.T, rm metricdata.ResourceMetrics, name string) float64 {
	t.Helper()
	sum := FindFloat64Sum(t, rm, name)
	var total float64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}

// FirstFloat64SumAttr returns the string value of an attribute on the first data point of a float64 sum metric.
func FirstFloat64SumAttr(t *testing.T, rm metricdata.ResourceMetrics, name, key string) string {
	t.Helper()
	sum := FindFloat64Sum(t, rm, name)
	require.NotEmpty(t, sum.DataPoints)
	return testutil.SpanStringAttr(t, sum.DataPoints[0].Attributes, key)
}

// AssertMetricAbsent fails if a metric with the given name exists.
func AssertMetricAbsent(t *testing.T, rm metricdata.ResourceMetrics, name string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				t.Fatalf("metric %q unexpectedly found", name)
			}
		}
	}
}

// FindInt64Histogram locates an int64 histogram metric by name.
func FindInt64Histogram(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Histogram[int64] {
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

// FindFloat64Histogram locates a float64 histogram metric by name.
func FindFloat64Histogram(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Histogram[float64] {
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

// FindFloat64Sum locates a float64 sum metric by name.
func FindFloat64Sum(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Sum[float64] {
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

// FindInt64Sum locates an int64 sum (counter) metric by name.
func FindInt64Sum(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Sum[int64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "metric %q is not Sum[int64]", name)
			return sum
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Sum[int64]{}
}

// Int64SumValue returns the total of all int64 sum data points (counter).
func Int64SumValue(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	sum := FindInt64Sum(t, rm, name)
	var total int64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}

// FirstInt64SumAttr returns a string attribute from the first counter data point.
func FirstInt64SumAttr(t *testing.T, rm metricdata.ResourceMetrics, name, key string) string {
	t.Helper()
	sum := FindInt64Sum(t, rm, name)
	require.NotEmpty(t, sum.DataPoints, "metric %q has no data points", name)
	return testutil.SpanStringAttr(t, sum.DataPoints[0].Attributes, key)
}

// FindGauge locates a float64 gauge metric by name.
func FindGauge(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Gauge[float64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[float64])
			require.True(t, ok, "metric %q is not Gauge[float64]", name)
			return gauge
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Gauge[float64]{}
}

// GaugeFloat64Value returns the value of the first gauge data point.
func GaugeFloat64Value(t *testing.T, rm metricdata.ResourceMetrics, name string) float64 {
	t.Helper()
	gauge := FindGauge(t, rm, name)
	require.NotEmpty(t, gauge.DataPoints, "metric %q has no data points", name)
	return gauge.DataPoints[0].Value
}

// FirstGaugeAttr returns a string attribute from the first gauge data point.
func FirstGaugeAttr(t *testing.T, rm metricdata.ResourceMetrics, name, key string) string {
	t.Helper()
	gauge := FindGauge(t, rm, name)
	require.NotEmpty(t, gauge.DataPoints)
	return testutil.SpanStringAttr(t, gauge.DataPoints[0].Attributes, key)
}
