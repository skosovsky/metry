package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestRecordAsyncFeedback_InvalidTraceID_ReturnsError(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTrackerWithTracer(nil, tp.Tracer("feedback"))
	require.NoError(t, err)
	installDefaultTrackerForTest(t, tracker)

	err = RecordAsyncFeedback(context.Background(), "invalid-trace-id", 0.5, "bad")
	require.Error(t, err)
}

func TestRecordAsyncFeedback_ValidTraceID_AttachesSpanWithoutPayloadByDefault(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTrackerWithTracer(nil, tp.Tracer("feedback"))
	require.NoError(t, err)
	installDefaultTrackerForTest(t, tracker)

	traceID := trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	err = RecordAsyncFeedback(context.Background(), traceID.String(), 0.9, "should-not-export")
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, "user_feedback", span.Name)
	assert.Equal(t, traceID, span.SpanContext.TraceID())
	assert.True(t, span.Parent.SpanID().IsValid())
	attrs := attribute.NewSet(span.Attributes...)
	assert.InDelta(t, 0.9, mustFloatAttr(t, attrs, EvaluationScoreKey), 1e-9)
	_, ok := attrs.Value(EvaluationTextKey)
	assert.False(t, ok)
}

func TestRecordAsyncFeedback_WithPayloadRecording_SetsText(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTrackerWithTracer(nil, tp.Tracer("feedback"), WithRecordPayloads(true))
	require.NoError(t, err)

	traceID := trace.TraceID{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}
	err = tracker.RecordAsyncFeedback(context.Background(), traceID.String(), 1.0, "approved")
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, "approved", mustStringAttr(t, attrs, EvaluationTextKey))
}

func TestRecordAsyncFeedback_NilContextPanics(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTrackerWithTracer(nil, tp.Tracer("feedback"))
	require.NoError(t, err)

	traceID := trace.TraceID{3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3}
	require.Panics(t, func() {
		//nolint:staticcheck // validating fail-fast behavior for nil context
		_ = tracker.RecordAsyncFeedback(nil, traceID.String(), 0.1, "boom")
	})
}

func TestRecordAsyncFeedback_UsesTrackerOwnedTracer(t *testing.T) {
	ambientExporter := tracetest.NewInMemoryExporter()
	ambientProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(ambientExporter))
	t.Cleanup(func() { _ = ambientProvider.Shutdown(context.Background()) })
	prevTracerProvider := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prevTracerProvider) })
	otel.SetTracerProvider(ambientProvider)

	ownedExporter := tracetest.NewInMemoryExporter()
	ownedProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(ownedExporter))
	t.Cleanup(func() { _ = ownedProvider.Shutdown(context.Background()) })

	tracker, err := NewTrackerWithTracer(nil, ownedProvider.Tracer("feedback"), WithRecordPayloads(true))
	require.NoError(t, err)

	traceID := trace.TraceID{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4}
	err = tracker.RecordAsyncFeedback(context.Background(), traceID.String(), 1.0, "owned")
	require.NoError(t, err)

	require.Empty(t, ambientExporter.GetSpans())
	spans := ownedExporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, traceID, spans[0].SpanContext.TraceID())
}

func TestRecordAsyncFeedback_DefaultTrackerIsTracingNoopBeforeInit(t *testing.T) {
	installDefaultTrackerForTest(t, nil)

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	prevTracerProvider := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prevTracerProvider) })
	otel.SetTracerProvider(tp)

	traceID := trace.TraceID{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5}
	err := RecordAsyncFeedback(context.Background(), traceID.String(), 0.5, "noop")
	require.NoError(t, err)
	require.Empty(t, mem.GetSpans())
}

func mustFloatAttr(t *testing.T, attrs attribute.Set, key attribute.Key) float64 {
	t.Helper()
	value, ok := attrs.Value(key)
	require.True(t, ok)
	return value.AsFloat64()
}
