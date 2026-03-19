package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestRecordAsyncFeedback_InvalidTraceID_ReturnsError(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	err := RecordAsyncFeedback(context.Background(), tp.Tracer("feedback"), "invalid-trace-id", 0.5, "bad")
	require.Error(t, err)
}

func TestRecordAsyncFeedback_ValidTraceID_AttachesSpanWithoutPayloadByDefault(t *testing.T) {
	t.Cleanup(resetRuntimeConfigForTest())

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	traceID := trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	err := RecordAsyncFeedback(context.Background(), tp.Tracer("feedback"), traceID.String(), 0.9, "should-not-export")
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, "user_feedback", span.Name)
	assert.Equal(t, traceID, span.SpanContext.TraceID())
	assert.True(t, span.Parent.SpanID().IsValid())
	attrs := attribute.NewSet(span.Attributes...)
	score, ok := attrs.Value(EvaluationScoreKey)
	require.True(t, ok)
	assert.InDelta(t, 0.9, score.AsFloat64(), 1e-9)
	_, ok = attrs.Value(EvaluationTextKey)
	assert.False(t, ok)
}

func TestRecordAsyncFeedback_WithPayloadRecording_SetsText(t *testing.T) {
	t.Cleanup(setRuntimeConfigForTest(defaultMaxContextLength, true))

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	traceID := trace.TraceID{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}
	err := RecordAsyncFeedback(context.Background(), tp.Tracer("feedback"), traceID.String(), 1.0, "approved")
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	text, ok := attrs.Value(EvaluationTextKey)
	require.True(t, ok)
	assert.Equal(t, "approved", text.AsString())
}
