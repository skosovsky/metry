package genai

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestRecordEvaluations_InvalidParent_ReturnsError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("evals"), tp.Tracer("evals"))
	require.NoError(t, err)

	err = tracker.RecordEvaluations(context.Background(), trace.SpanContext{}, []Evaluation{
		{Metric: EvaluationFaithfulness, Score: 0.8},
	})
	require.ErrorIs(t, err, ErrParentSpanContextRequired)
}

func TestRecordEvaluations_ValidParent_AddsChildSpanAndEvents(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("evals"), tp.Tracer("evals"))
	require.NoError(t, err)

	parent := newParentSpanContext(true)
	err = tracker.RecordEvaluations(context.Background(), parent, []Evaluation{
		{Metric: EvaluationFaithfulness, Score: 0.91, Reasoning: "hidden"},
		{Metric: EvaluationAnswerRelevance, Score: 0.76},
	})
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "llm_evaluations", spans[0].Name)
	assert.Equal(t, parent.TraceID(), spans[0].SpanContext.TraceID())
	assert.Equal(t, parent.SpanID(), spans[0].Parent.SpanID())

	spanAttrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, PurposeQualityEvaluation, mustStringAttr(t, spanAttrs, OperationPurposeKey))

	require.Len(t, spans[0].Events, 2)
	assert.Equal(t, "evaluation", spans[0].Events[0].Name)
	assert.Equal(t, "evaluation", spans[0].Events[1].Name)

	firstAttrs := attribute.NewSet(spans[0].Events[0].Attributes...)
	assert.Equal(t, string(EvaluationFaithfulness), mustStringAttr(t, firstAttrs, EvaluationMetricNameKey))
	assert.InDelta(t, 0.91, mustFloatAttr(t, firstAttrs, EvaluationScoreKey), 1e-9)
	_, ok := firstAttrs.Value(EvaluationReasoningKey)
	assert.False(t, ok)
}

func TestRecordEvaluations_WithPayloadRecording_IncludesReasoning(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("evals"),
		tp.Tracer("evals"),
		WithRecordPayloads(true),
		WithMaxContextLength(48),
	)
	require.NoError(t, err)

	parent := newParentSpanContext(false)
	err = tracker.RecordEvaluations(context.Background(), parent, []Evaluation{
		{
			Metric:    EvaluationContextPrecision,
			Score:     0.67,
			Reasoning: strings.Repeat("x", 200),
		},
	})
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)

	eventAttrs := attribute.NewSet(spans[0].Events[0].Attributes...)
	reasoning := mustStringAttr(t, eventAttrs, EvaluationReasoningKey)
	assert.LessOrEqual(t, len(reasoning), 48)
	assert.True(t, utf8.ValidString(reasoning))
}

func TestRecordEvaluations_WithKeepHint_ExportsSpanWhenBaseSamplerDrops(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(mem),
		sdktrace.WithSampler(NewHintSampler(sdktrace.NeverSample())),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("evals"), tp.Tracer("evals"))
	require.NoError(t, err)

	parent := unsampledRemoteParentSpanContext()
	err = tracker.RecordEvaluations(
		context.Background(),
		parent,
		[]Evaluation{{Metric: EvaluationFaithfulness, Score: 0.2}},
		trace.WithAttributes(SamplingKeepKey.Bool(true)),
	)
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "llm_evaluations", spans[0].Name)
	assert.Equal(t, parent.TraceID(), spans[0].SpanContext.TraceID())
	assert.Equal(t, parent.SpanID(), spans[0].Parent.SpanID())
}

func TestRecordEvaluations_WithoutKeepHint_DroppedWhenBaseSamplerDrops(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(mem),
		sdktrace.WithSampler(NewHintSampler(sdktrace.NeverSample())),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("evals"), tp.Tracer("evals"))
	require.NoError(t, err)

	parent := unsampledRemoteParentSpanContext()
	err = tracker.RecordEvaluations(
		context.Background(),
		parent,
		[]Evaluation{{Metric: EvaluationFaithfulness, Score: 0.2}},
	)
	require.NoError(t, err)

	require.Empty(t, mem.GetSpans())
}
