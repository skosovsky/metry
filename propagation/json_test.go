package propagation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
)

func TestInjectExtract_RoundTrip_PreservesTraceAndBaggage(t *testing.T) {
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(prop)
	t.Cleanup(func() {
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	})

	ctx := context.Background()
	ctx, err := metry.SetBaggageValue(ctx, "job", "42")
	require.NoError(t, err)

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("propagation-test")

	ctx, span := tracer.Start(ctx, "producer")
	sc := span.SpanContext()
	span.End()

	payload, err := InjectToJSONWithPropagator(ctx, prop)
	require.NoError(t, err)
	require.NotEmpty(t, payload)

	outCtx := ExtractFromJSONWithPropagator(context.Background(), prop, payload)
	outSc := trace.SpanContextFromContext(outCtx)
	require.True(t, outSc.IsValid())
	assert.Equal(t, sc.TraceID(), outSc.TraceID())
	assert.Equal(t, sc.SpanID(), outSc.SpanID())
	assert.Equal(t, "42", metry.BaggageValue(outCtx, "job"))
}

func TestExtractFromJSON_InvalidPayload_ReturnsOriginalContext(t *testing.T) {
	prop := propagation.TraceContext{}
	ctx := context.Background()
	got := ExtractFromJSONWithPropagator(ctx, prop, []byte("{not-json"))
	assert.Equal(t, ctx, got)
}

func TestExtractFromJSON_EmptyPayload_ReturnsOriginalContext(t *testing.T) {
	prop := propagation.TraceContext{}
	ctx := context.Background()
	got := ExtractFromJSONWithPropagator(ctx, prop, nil)
	assert.Equal(t, ctx, got)
	got = ExtractFromJSONWithPropagator(ctx, prop, []byte{})
	assert.Equal(t, ctx, got)
}

func TestInjectToJSON_EmptyContext_ReturnsEmptyObjectJSON(t *testing.T) {
	prop := propagation.TraceContext{}
	payload, err := InjectToJSONWithPropagator(context.Background(), prop)
	require.NoError(t, err)
	assert.JSONEq(t, "{}", string(payload))
}

func TestInjectExtract_WithInMemoryExporter(t *testing.T) {
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{})
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("test").Start(context.Background(), "root")
	payload, err := InjectToJSONWithPropagator(ctx, prop)
	require.NoError(t, err)
	span.End()

	childCtx := ExtractFromJSONWithPropagator(context.Background(), prop, payload)
	_, child := tp.Tracer("test").Start(childCtx, "consumer")
	child.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, spans[0].SpanContext.TraceID(), spans[1].SpanContext.TraceID())
}
