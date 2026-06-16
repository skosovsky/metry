package propagation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/testutil"
)

func TestInjectExtractMap_RoundTrip_PreservesTraceAndBaggage(t *testing.T) {
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	ctx := context.Background()
	member, err := baggage.NewMember("subject_id", "job-42")
	require.NoError(t, err)
	b, err := baggage.New()
	require.NoError(t, err)
	b, err = b.SetMember(member)
	require.NoError(t, err)
	ctx = baggage.ContextWithBaggage(ctx, b)

	mem := testutil.NewInMemoryTraceExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem.SDKSpanExporter()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("propagation-test").Start(ctx, "producer")
	sc := span.SpanContext()
	span.End()

	carrier := map[string]any{"order_id": "ord-1", "payload": 123}
	InjectToMap(ctx, prop, carrier)

	outCtx := ExtractFromMap(context.Background(), prop, carrier)
	outSc := trace.SpanContextFromContext(outCtx)
	require.True(t, outSc.IsValid())
	assert.Equal(t, sc.TraceID(), outSc.TraceID())
	assert.Equal(t, sc.SpanID(), outSc.SpanID())
	assert.Equal(t, "job-42", baggage.FromContext(outCtx).Member("subject_id").Value())
	assert.Equal(t, "ord-1", carrier["order_id"])
	assert.Equal(t, 123, carrier["payload"])
}

func TestExtractFromMap_EmptyCarrier_ReturnsOriginalContext(t *testing.T) {
	prop := propagation.TraceContext{}
	ctx := context.Background()
	got := ExtractFromMap(ctx, prop, nil)
	assert.Equal(t, ctx, got)
	got = ExtractFromMap(ctx, prop, map[string]any{})
	assert.Equal(t, ctx, got)
}

func TestExtractFromMap_IgnoresNonStringValues(t *testing.T) {
	prop := propagation.TraceContext{}
	ctx := context.Background()
	carrier := map[string]any{"traceparent": 12345, "host_key": "keep"}
	got := ExtractFromMap(ctx, prop, carrier)
	assert.Equal(t, ctx, got)
}

func TestExtractFromMap_IgnoresNonW3CBusinessKeys(t *testing.T) {
	prop := propagation.TraceContext{}
	ctx := context.Background()
	carrier := map[string]any{"order_id": "ord-99", "custom_trace": "fake-parent"}
	got := ExtractFromMap(ctx, prop, carrier)
	outSc := trace.SpanContextFromContext(got)
	assert.False(t, outSc.IsValid())
}

func TestInjectExtractMap_WithInMemoryExporter(t *testing.T) {
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{})
	mem := testutil.NewInMemoryTraceExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem.SDKSpanExporter()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("test").Start(context.Background(), "root")
	carrier := map[string]any{}
	InjectToMap(ctx, prop, carrier)
	span.End()

	childCtx := ExtractFromMap(context.Background(), prop, carrier)
	_, child := tp.Tracer("test").Start(childCtx, "consumer")
	child.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, spans[0].SpanContext.TraceID(), spans[1].SpanContext.TraceID())
}
