package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestRecordAsyncFeedback_InvalidParent_ReturnsError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("feedback"), tp.Tracer("feedback"))
	require.NoError(t, err)

	err = tracker.RecordAsyncFeedback(context.Background(), trace.SpanContext{}, 0.5, "bad")
	require.ErrorIs(t, err, ErrParentSpanContextRequired)
}

func TestRecordAsyncFeedback_ValidRemoteParent_AttachesSpan(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("feedback"), tp.Tracer("feedback"))
	require.NoError(t, err)

	parent := newParentSpanContext(true)
	err = tracker.RecordAsyncFeedback(context.Background(), parent, 0.9, "should-not-export")
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "user_feedback", spans[0].Name)
	assert.Equal(t, parent.TraceID(), spans[0].SpanContext.TraceID())
	assert.Equal(t, parent.SpanID(), spans[0].Parent.SpanID())
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.InDelta(t, 0.9, mustFloatAttr(t, attrs, EvaluationScoreKey), 1e-9)
	_, ok := attrs.Value(EvaluationTextKey)
	assert.False(t, ok)
}

func TestRecordAsyncFeedback_WithPayloadRecording_SetsText(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("feedback"),
		tp.Tracer("feedback"),
		WithRecordPayloads(true),
	)
	require.NoError(t, err)

	parent := newParentSpanContext(false)
	err = tracker.RecordAsyncFeedback(context.Background(), parent, 1.0, "approved")
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, "approved", mustStringAttr(t, attrs, EvaluationTextKey))
}

func TestRecordAsyncFeedback_ValidLocalParent_AttachesSpan(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("feedback"), tp.Tracer("feedback"))
	require.NoError(t, err)

	parent := newParentSpanContext(false)
	err = tracker.RecordAsyncFeedback(context.Background(), parent, 0.7, "local-parent")
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, parent.TraceID(), spans[0].SpanContext.TraceID())
	assert.Equal(t, parent.SpanID(), spans[0].Parent.SpanID())
}
