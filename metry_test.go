package metry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"

	"github.com/skosovsky/metry/internal/metrytestwire"
	"github.com/skosovsky/metry/testutil"
)

func TestNew_MissingServiceName_ReturnsError(t *testing.T) {
	provider, err := New(context.Background(), WithEnvironment("test"))
	require.ErrorIs(t, err, ErrServiceNameRequired)
	require.Nil(t, provider)
}

func TestNew_MeterProvider_BehaviorWithAndWithoutMetricExporter(t *testing.T) {
	ctx := context.Background()

	withoutMetrics, err := New(ctx, WithServiceName("test-no-metrics"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = withoutMetrics.Shutdown(ctx) })
	require.NotNil(t, withoutMetrics.meterProvider())
	require.Nil(t, withoutMetrics.mp)

	metricExporter := testutil.NewInMemoryMetricExporter()
	withMetrics, err := New(
		ctx,
		WithServiceName("test-with-metrics"),
		WithMetricExporter(mustMetricExporter(metricExporter)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = withMetrics.Shutdown(ctx) })
	require.NotNil(t, withMetrics.meterProvider())
	require.NotNil(t, withMetrics.mp)
}

func TestProvider_ContainsW3CPropagator(t *testing.T) {
	ctx := context.Background()
	provider, err := New(ctx, WithServiceName("test-svc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	fields := provider.textMapPropagator().Fields()
	require.Contains(t, fields, propagation.TraceContext{}.Fields()[0])
}

func TestProvider_Shutdown_NilSafe(t *testing.T) {
	var provider *Provider
	require.NoError(t, provider.Shutdown(context.Background()))
}

func mustSpanExporter(mem *testutil.InMemoryTraceExporter) SpanExporter {
	v := metrytestwire.SpanExporter(mem.SDKSpanExporter())
	e, ok := v.(SpanExporter)
	if !ok || v == nil {
		panic("metry: test SpanExporter wire hook returned unexpected value")
	}
	return e
}

func mustMetricExporter(mem *testutil.InMemoryMetricExporter) MetricExporter {
	v := metrytestwire.MetricExporter(mem.SDKExporter())
	e, ok := v.(MetricExporter)
	if !ok || v == nil {
		panic("metry: test MetricExporter wire hook returned unexpected value")
	}
	return e
}
