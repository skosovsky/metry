package testutil

import (
	"context"
	"testing"

	"github.com/skosovsky/metry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestNewInMemoryTraceExporter_InitialState(t *testing.T) {
	mem := NewInMemoryTraceExporter()
	require.NotNil(t, mem)
	assert.Equal(t, 0, mem.Len())
	spans := mem.GetSpans()
	assert.Empty(t, spans)
	mem.Reset()
	assert.Equal(t, 0, mem.Len())
}

func TestInMemoryTraceExporter_TraceExporter(t *testing.T) {
	mem := NewInMemoryTraceExporter()
	te := mem.TraceExporter()
	require.NotNil(t, te)
	// Use SimpleSpanProcessor so spans are exported synchronously for deterministic test.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(mem.ex)),
	)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := metry.GlobalTracer()
	_, span := tracer.Start(context.Background(), "op")
	span.End()
	assert.Equal(t, 1, mem.Len())
}

func TestSetupTestTracing_ReturnsExporter(t *testing.T) {
	mem := SetupTestTracing(t)
	require.NotNil(t, mem)
	tracer := metry.GlobalTracer()
	require.NotNil(t, tracer)
	_, span := tracer.Start(context.Background(), "test")
	span.End()
	assert.Equal(t, 1, mem.Len())
}

func TestSetupTestMetrics_ReturnsReaderAndMeter(t *testing.T) {
	reader, meter := SetupTestMetrics(t)
	require.NotNil(t, reader)
	require.NotNil(t, meter)
}

func TestInMemoryMetricExporter_GetMetrics_Reset_Len(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	require.NotNil(t, mem)
	assert.Equal(t, 0, mem.Len())

	ctx := context.Background()
	err := mem.Export(ctx, &metricdata.ResourceMetrics{})
	require.NoError(t, err)
	assert.Equal(t, 1, mem.Len())
	assert.Equal(t, 1, mem.GetMetrics())

	mem.Reset()
	assert.Equal(t, 0, mem.Len())
}
