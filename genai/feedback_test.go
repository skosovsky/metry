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

func TestRecordAsyncFeedback_InvalidLinkedContext_ReturnsError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("feedback"), tp.Tracer("feedback"))
	require.NoError(t, err)

	err = tracker.RecordAsyncFeedback(context.Background(), trace.SpanContext{}, 0.5, "bad")
	require.ErrorIs(t, err, ErrInvalidSpanContext)
}

func TestRecordAsyncFeedback_ValidRemoteLinked_AttachesSpanWithLink(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("feedback"), tp.Tracer("feedback"))
	require.NoError(t, err)

	linked := newParentSpanContext(true)
	err = tracker.RecordAsyncFeedback(context.Background(), linked, 0.9, "should-not-export")
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "user_feedback", spans[0].Name)
	assertLinkBasedAsyncSpan(t, spans[0], linked)
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

	linked := newParentSpanContext(false)
	err = tracker.RecordAsyncFeedback(context.Background(), linked, 1.0, "approved")
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, "approved", mustStringAttr(t, attrs, EvaluationTextKey))
}

func TestRecordAsyncFeedback_IgnoresContextParent_UsesLinkOnly(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("feedback"), tp.Tracer("feedback"))
	require.NoError(t, err)

	ctx, active := tp.Tracer("feedback").Start(context.Background(), "interaction")
	linked := newParentSpanContext(false)
	active.End()

	err = tracker.RecordAsyncFeedback(ctx, linked, 0.7, "local-parent")
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	feedback := spans[1]
	assert.Equal(t, "user_feedback", feedback.Name)
	assertLinkBasedAsyncSpan(t, feedback, linked)
}

func TestRecordAsyncFeedback_WithKeepHint_ExportsSpanWhenBaseSamplerDrops(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(mem),
		sdktrace.WithSampler(NewHintSampler(sdktrace.NeverSample())),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("feedback"), tp.Tracer("feedback"))
	require.NoError(t, err)

	linked := unsampledRemoteParentSpanContext()
	err = tracker.RecordAsyncFeedback(
		context.Background(),
		linked,
		0.2,
		"negative",
		trace.WithAttributes(SamplingKeepKey.Bool(true)),
	)
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "user_feedback", spans[0].Name)
	assertLinkBasedAsyncSpan(t, spans[0], linked)
}

func TestRecordAsyncFeedback_WithoutKeepHint_DroppedWhenBaseSamplerDrops(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(mem),
		sdktrace.WithSampler(NewHintSampler(sdktrace.NeverSample())),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("feedback"), tp.Tracer("feedback"))
	require.NoError(t, err)

	linked := unsampledRemoteParentSpanContext()
	err = tracker.RecordAsyncFeedback(context.Background(), linked, 0.2, "negative")
	require.NoError(t, err)

	require.Empty(t, mem.GetSpans())
}

func unsampledRemoteParentSpanContext() trace.SpanContext {
	traceID := trace.TraceID{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}
	spanID := trace.SpanID{8, 8, 8, 8, 8, 8, 8, 8}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: 0,
		Remote:     true,
	})
}
