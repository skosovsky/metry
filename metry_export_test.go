package metry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestNew_ProviderWorksAndShutdownIsIdempotent(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("test-svc"))

	_, end, err := provider.StartSpan(ctx, "metry-test", "span")
	require.NoError(t, err)
	end()

	require.NoError(t, provider.ForceFlush(ctx))
	require.GreaterOrEqual(t, mem.Len(), 1)

	require.NoError(t, provider.Shutdown(ctx))
	require.NoError(t, provider.Shutdown(ctx))
}

func TestNew_NoTraceExporter_TracerStartsAndShutdowns(t *testing.T) {
	ctx := context.Background()
	provider, err := metry.New(ctx, metry.WithServiceName("test-svc"))
	require.NoError(t, err)

	_, end, err := provider.StartSpan(ctx, "metry-test", "span-no-exporter")
	require.NoError(t, err)
	end()

	require.NoError(t, provider.Shutdown(ctx))
	require.NoError(t, provider.Shutdown(ctx))
}

func TestNew_WithSampler_OverridesTraceRatio(t *testing.T) {
	ctx := context.Background()

	providerNever, memNever := metrytest.NewTestProvider(t,
		metry.WithServiceName("test-sampler-never"),
		metry.WithTraceRatio(1.0),
		metry.WithSampler(metry.NeverSample()),
	)
	_, end, err := providerNever.StartSpan(ctx, "metry-test", "span-never")
	require.NoError(t, err)
	end()
	require.NoError(t, providerNever.ForceFlush(ctx))
	require.Empty(t, memNever.GetSpans())

	providerAlways, memAlways := metrytest.NewTestProvider(t,
		metry.WithServiceName("test-sampler-always"),
		metry.WithTraceRatio(0.0),
		metry.WithSampler(metry.AlwaysSample()),
	)
	_, end, err = providerAlways.StartSpan(ctx, "metry-test", "span-always")
	require.NoError(t, err)
	end()
	require.NoError(t, providerAlways.ForceFlush(ctx))
	require.Len(t, memAlways.GetSpans(), 1)
	require.Equal(t, "span-always", memAlways.GetSpans()[0].Name)
}

func TestProvider_StartSpan_Success_SetsOkStatus(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("test-svc"))

	_, end, err := provider.StartSpan(ctx, "metry-test", "span-ok")
	require.NoError(t, err)
	end()

	require.NoError(t, provider.ForceFlush(ctx))
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	testutil.AssertSpanStubOkStatus(t, spans[0])
}

func TestNew_DoesNotMutateGlobalOTelState(t *testing.T) {
	ctx := context.Background()
	prevTracerProvider := otel.GetTracerProvider()
	prevMeterProvider := otel.GetMeterProvider()
	prevPropagator := otel.GetTextMapPropagator()

	provider, err := metry.New(ctx, metry.WithServiceName("test-svc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	require.Same(t, prevTracerProvider, otel.GetTracerProvider())
	require.Same(t, prevMeterProvider, otel.GetMeterProvider())
	require.Same(t, prevPropagator, otel.GetTextMapPropagator())
}
