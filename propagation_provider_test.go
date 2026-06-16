package metry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/testutil"
)

func TestProvider_InjectExtractMap_RoundTrip(t *testing.T) {
	ctx := context.Background()
	provider, mem := newTestProvider(t, WithServiceName("prop-test"))

	ctx, end, err := provider.StartSpan(ctx, "test", "root")
	require.NoError(t, err)
	carrier := map[string]any{"biz": "data"}
	provider.InjectToMap(ctx, carrier)
	end()

	childCtx := provider.ExtractFromMap(context.Background(), carrier)
	_, childEnd, err := provider.StartSpan(childCtx, "test", "child")
	require.NoError(t, err)
	childEnd()

	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, spans[0].SpanContext.TraceID(), spans[1].SpanContext.TraceID())
	assert.NotNil(t, provider.textMapPropagator())
}

func TestProvider_ExtractFromMap_IgnoresBusinessKeysOnly(t *testing.T) {
	provider, _ := newTestProvider(t)
	got := provider.ExtractFromMap(context.Background(), map[string]any{"order_id": "ord-1"})
	assert.False(t, trace.SpanContextFromContext(got).IsValid())
}

func TestProvider_InjectExtractMap_NilPropagatorField_UsesDefault(t *testing.T) {
	mem := testutil.NewInMemoryTraceExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem.SDKSpanExporter()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	provider := &Provider{otelTracer: tp}
	ctx, end, err := provider.StartSpan(context.Background(), "test", "root")
	require.NoError(t, err)
	carrier := map[string]any{"biz": "data"}
	provider.InjectToMap(ctx, carrier)
	end()

	childCtx := provider.ExtractFromMap(context.Background(), carrier)
	_, childEnd, err := provider.StartSpan(childCtx, "test", "child")
	require.NoError(t, err)
	childEnd()

	require.NoError(t, provider.ForceFlush(context.Background()))
	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, spans[0].SpanContext.TraceID(), spans[1].SpanContext.TraceID())
}
