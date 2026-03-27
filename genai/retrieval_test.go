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

func TestStartRetrievalSpan_SetsAttributesAndParent(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("genai-retrieval"),
		tp.Tracer("genai-retrieval"),
		WithRecordPayloads(true),
		WithMaxContextLength(32),
	)
	require.NoError(t, err)

	parentCtx, parent := tp.Tracer("genai-retrieval").Start(context.Background(), "request")
	ctx, retrievalSpan := tracker.StartRetrievalSpan(parentCtx, "vector.search", RetrievalRequest{
		Provider: "qdrant",
		Source:   "knowledge_base",
		Query:    strings.Repeat("a", 128),
		TopK:     5,
	})
	require.NotNil(t, ctx)
	tracker.RecordRetrievalResult(retrievalSpan, RetrievalResult{
		ReturnedChunks: 0,
		Distances:      []float64{0.9, 0.8},
	})
	retrievalSpan.End()
	parent.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	retrieval := spanByName(t, spans, "vector.search")
	attrs := attribute.NewSet(retrieval.Attributes...)

	assert.Equal(t, parent.SpanContext().SpanID(), retrieval.Parent.SpanID())
	assert.Equal(t, "retrieval", mustStringAttr(t, attrs, OperationNameKey))
	assert.Equal(t, "qdrant", mustStringAttr(t, attrs, RetrievalProviderKey))
	assert.Equal(t, "knowledge_base", mustStringAttr(t, attrs, RetrievalSourceKey))
	assert.Equal(t, int64(5), mustIntAttr(t, attrs, RetrievalTopKKey))

	query := mustStringAttr(t, attrs, RetrievalQueryKey)
	assert.LessOrEqual(t, len(query), 32)
	assert.True(t, utf8.ValidString(query))

	assert.Equal(t, int64(0), mustIntAttr(t, attrs, RetrievalReturnedChunksKey))
	distances, ok := attrs.Value(RetrievalDistancesKey)
	require.True(t, ok)
	assert.Equal(t, []float64{0.9, 0.8}, distances.AsFloat64Slice())
}

func TestStartRetrievalSpan_WithoutRecordPayloads_DoesNotSetQuery(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("genai-retrieval"),
		tp.Tracer("genai-retrieval"),
		WithRecordPayloads(false),
	)
	require.NoError(t, err)

	_, span := tracker.StartRetrievalSpan(context.Background(), "vector.search", RetrievalRequest{
		Provider: "qdrant",
		Query:    "sensitive",
		TopK:     5,
	})
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	_, ok := attrs.Value(RetrievalQueryKey)
	assert.False(t, ok)
}

func TestStartRetrievalSpan_WithKeepHint_ExportsSpanWhenBaseSamplerDrops(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(mem),
		sdktrace.WithSampler(NewHintSampler(sdktrace.NeverSample())),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("genai-retrieval"),
		tp.Tracer("genai-retrieval"),
	)
	require.NoError(t, err)

	_, span := tracker.StartRetrievalSpan(
		context.Background(),
		"vector.search",
		RetrievalRequest{Provider: "qdrant", TopK: 3},
		trace.WithAttributes(SamplingKeepKey.Bool(true)),
	)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, "qdrant", mustStringAttr(t, attrs, RetrievalProviderKey))
	assert.True(t, spans[0].SpanContext.IsSampled())
}

func TestStartRetrievalSpan_WithCallerAttributes_PreservesBuiltInAttributes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("genai-retrieval"),
		tp.Tracer("genai-retrieval"),
	)
	require.NoError(t, err)

	callerKey := attribute.Key("test.retrieval.caller")
	_, span := tracker.StartRetrievalSpan(
		context.Background(),
		"vector.search",
		RetrievalRequest{Provider: "qdrant", Source: "knowledge_base", TopK: 3},
		trace.WithAttributes(callerKey.String("present")),
	)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, "retrieval", mustStringAttr(t, attrs, OperationNameKey))
	assert.Equal(t, "qdrant", mustStringAttr(t, attrs, RetrievalProviderKey))
	assert.Equal(t, "knowledge_base", mustStringAttr(t, attrs, RetrievalSourceKey))
	assert.Equal(t, "present", mustStringAttr(t, attrs, callerKey))
}

func TestStartRetrievalSpan_WithDuplicateCallerKeys_BuiltInWins(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("genai-retrieval"),
		tp.Tracer("genai-retrieval"),
	)
	require.NoError(t, err)

	_, span := tracker.StartRetrievalSpan(
		context.Background(),
		"vector.search",
		RetrievalRequest{Provider: "qdrant", Source: "knowledge_base", TopK: 3},
		trace.WithAttributes(
			OperationNameKey.String("override"),
			RetrievalProviderKey.String("override"),
			RetrievalSourceKey.String("override"),
			RetrievalTopKKey.Int(99),
		),
	)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, "retrieval", mustStringAttr(t, attrs, OperationNameKey))
	assert.Equal(t, "qdrant", mustStringAttr(t, attrs, RetrievalProviderKey))
	assert.Equal(t, "knowledge_base", mustStringAttr(t, attrs, RetrievalSourceKey))
	assert.Equal(t, int64(3), mustIntAttr(t, attrs, RetrievalTopKKey))
}

func spanByName(t *testing.T, spans []tracetest.SpanStub, name string) tracetest.SpanStub {
	t.Helper()
	for _, span := range spans {
		if span.Name == name {
			return span
		}
	}
	t.Fatalf("span with name %q not found", name)
	return tracetest.SpanStub{}
}
