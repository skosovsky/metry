package metry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/testutil"
)

func TestNew_MissingServiceName_ReturnsError(t *testing.T) {
	provider, err := New(context.Background(), WithEnvironment("test"))
	require.ErrorIs(t, err, ErrServiceNameRequired)
	require.Nil(t, provider)
}

func TestNew_DoesNotMutateGlobalOTelState(t *testing.T) {
	ctx := context.Background()
	prevTracerProvider := otel.GetTracerProvider()
	prevMeterProvider := otel.GetMeterProvider()
	prevPropagator := otel.GetTextMapPropagator()

	provider, err := New(ctx, WithServiceName("test-svc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	require.Same(t, prevTracerProvider, otel.GetTracerProvider())
	require.Same(t, prevMeterProvider, otel.GetMeterProvider())
	require.Same(t, prevPropagator, otel.GetTextMapPropagator())
}

func TestNew_ProviderWorksAndShutdownIsIdempotent(t *testing.T) {
	ctx := context.Background()
	traceExporter := tracetest.NewInMemoryExporter()
	provider, err := New(
		ctx,
		WithServiceName("test-svc"),
		WithExporter(traceExporter),
	)
	require.NoError(t, err)

	_, span := provider.TracerProvider.Tracer("metry-test").Start(ctx, "span")
	span.End()

	tp := provider.TracerProvider.(*sdktrace.TracerProvider)
	require.NoError(t, tp.ForceFlush(ctx))
	require.Len(t, traceExporter.GetSpans(), 1)

	require.NoError(t, provider.Shutdown(ctx))
	require.NoError(t, provider.Shutdown(ctx))
}

func TestNew_NoTraceExporter_TracerStartsAndShutdowns(t *testing.T) {
	ctx := context.Background()
	provider, err := New(ctx, WithServiceName("test-svc"))
	require.NoError(t, err)

	_, span := provider.TracerProvider.Tracer("metry-test").Start(ctx, "span-no-exporter")
	span.End()

	require.NoError(t, provider.Shutdown(ctx))
	require.NoError(t, provider.Shutdown(ctx))
}

func TestNew_MeterProvider_BehaviorWithAndWithoutMetricExporter(t *testing.T) {
	ctx := context.Background()

	withoutMetrics, err := New(ctx, WithServiceName("test-no-metrics"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = withoutMetrics.Shutdown(ctx) })
	require.NotNil(t, withoutMetrics.MeterProvider)
	require.Nil(t, withoutMetrics.mp)

	metricExporter := testutil.NewInMemoryMetricExporter()
	withMetrics, err := New(
		ctx,
		WithServiceName("test-with-metrics"),
		WithMetricExporter(metricExporter.Exporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = withMetrics.Shutdown(ctx) })
	require.NotNil(t, withMetrics.MeterProvider)
	require.NotNil(t, withMetrics.mp)
}

func TestNew_NoMetricExporter_TrackerWorksViaPublicAPI(t *testing.T) {
	ctx := context.Background()
	traceExporter := tracetest.NewInMemoryExporter()

	provider, err := New(
		ctx,
		WithServiceName("test-no-metrics-tracker"),
		WithExporter(traceExporter),
	)
	require.NoError(t, err)

	tracker, err := genai.NewTracker(
		provider.MeterProvider.Meter("metry/genai"),
		provider.TracerProvider.Tracer("metry/genai"),
	)
	require.NoError(t, err)

	_, span := provider.TracerProvider.Tracer("metry-test").Start(ctx, "interaction-span")
	tracker.RecordInteraction(ctx, span, genai.Meta{
		Provider:  "openai",
		Operation: "chat",
	}, genai.Payload{}, genai.Usage{
		InputTokens:  1,
		OutputTokens: 1,
	})
	span.End()

	tp := provider.TracerProvider.(*sdktrace.TracerProvider)
	require.NoError(t, tp.ForceFlush(ctx))
	require.NotEmpty(t, traceExporter.GetSpans())

	require.NoError(t, provider.Shutdown(ctx))
}

func TestProvider_ContainsW3CPropagator(t *testing.T) {
	ctx := context.Background()
	provider, err := New(ctx, WithServiceName("test-svc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	fields := provider.Propagator.Fields()
	require.Contains(t, fields, propagation.TraceContext{}.Fields()[0])
}

func TestProvider_Shutdown_NilSafe(t *testing.T) {
	var provider *Provider
	require.NoError(t, provider.Shutdown(context.Background()))
}

func TestProvider_TracerProvider_IsSDKTracerProvider(t *testing.T) {
	provider, err := New(context.Background(), WithServiceName("test-svc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	_, ok := provider.TracerProvider.(*sdktrace.TracerProvider)
	require.True(t, ok)
}
