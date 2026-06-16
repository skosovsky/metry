package genai

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/testutil"
)

func TestStartRetrievalSpan_SetsAttributesAndParent(t *testing.T) {
	tracker, provider, mem := newTestTracker(t,
		WithRecordPayloads(true),
		WithMaxContextLength(32),
	)

	parentCtx, endParent, err := provider.StartSpan(context.Background(), "genai-retrieval", "request")
	require.NoError(t, err)
	ctx, end := tracker.StartRetrievalSpan(parentCtx, "vector.search", RetrievalRequest{
		Provider: "qdrant",
		Source:   "knowledge_base",
		Query:    strings.Repeat("a", 128),
		TopK:     5,
	})
	require.NotNil(t, ctx)
	tracker.RecordRetrievalResult(ctx, RetrievalResult{
		ReturnedChunks: 0,
		Distances:      []float64{0.9, 0.8},
	})
	end()
	endParent()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	retrieval := testutil.SpanByName(t, spans, "vector.search")
	request := testutil.SpanByName(t, spans, "request")

	assert.Equal(t, request.SpanContext.SpanID(), retrieval.Parent.SpanID())
	assert.Equal(t, "retrieval", testutil.SpanStubStringAttr(t, retrieval, OperationName))
	assert.Equal(t, "qdrant", testutil.SpanStubStringAttr(t, retrieval, RetrievalProvider))
	assert.Equal(t, "knowledge_base", testutil.SpanStubStringAttr(t, retrieval, RetrievalSource))
	assert.Equal(t, int64(5), testutil.SpanStubInt64Attr(t, retrieval, RetrievalTopK))

	query := testutil.SpanStubStringAttr(t, retrieval, RetrievalQuery)
	assert.LessOrEqual(t, len(query), 32)
	assert.True(t, utf8.ValidString(query))

	assert.Equal(t, int64(0), testutil.SpanStubInt64Attr(t, retrieval, RetrievalReturnedChunks))
	assert.Equal(t, []float64{0.9, 0.8}, testutil.SpanStubFloat64SliceAttr(t, retrieval, RetrievalDistances))
	testutil.AssertSpanStubOkStatus(t, retrieval)
}

func TestRecordRetrievalResult_SetsOkStatus(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	ctx, end := tracker.StartRetrievalSpan(context.Background(), "vector.search", RetrievalRequest{
		Provider: "qdrant",
		TopK:     3,
	})
	tracker.RecordRetrievalResult(ctx, RetrievalResult{ReturnedChunks: 2})
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	testutil.AssertSpanStubOkStatus(t, spans[0])
}

func TestStartRetrievalSpan_WithoutRecordPayloads_DoesNotSetQuery(t *testing.T) {
	tracker, provider, mem := newTestTracker(t, WithRecordPayloads(false))

	_, end := tracker.StartRetrievalSpan(context.Background(), "vector.search", RetrievalRequest{
		Provider: "qdrant",
		Query:    "sensitive",
		TopK:     5,
	})
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalQuery))
}

func TestStartRetrievalSpan_WithKeepHint_ExportsSpanWhenBaseSamplerDrops(t *testing.T) {
	tracker, provider, mem := newTestTrackerWithSampler(t, NewHintSampler(metry.NeverSample()))

	_, end := tracker.StartRetrievalSpan(
		context.Background(),
		"vector.search",
		RetrievalRequest{Provider: "qdrant", TopK: 3},
		WithSpanSamplingKeep(),
	)
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "qdrant", testutil.SpanStubStringAttr(t, spans[0], RetrievalProvider))
	assert.True(t, spans[0].SpanContext.IsSampled())
}

func TestStartRetrievalSpan_WithCallerAttributes_PreservesBuiltInAttributes(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	_, end := tracker.StartRetrievalSpan(
		context.Background(),
		"vector.search",
		RetrievalRequest{Provider: "qdrant", Source: "knowledge_base", TopK: 3},
		WithSpanAttributes(metry.StringAttribute("test.retrieval.caller", "present")),
	)
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "retrieval", testutil.SpanStubStringAttr(t, spans[0], OperationName))
	assert.Equal(t, "qdrant", testutil.SpanStubStringAttr(t, spans[0], RetrievalProvider))
	assert.Equal(t, "knowledge_base", testutil.SpanStubStringAttr(t, spans[0], RetrievalSource))
	assert.Equal(t, "present", testutil.SpanStubStringAttr(t, spans[0], "test.retrieval.caller"))
}

func TestStartRetrievalSpan_WithDuplicateCallerKeys_BuiltInWins(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	_, end := tracker.StartRetrievalSpan(
		context.Background(),
		"vector.search",
		RetrievalRequest{Provider: "qdrant", Source: "knowledge_base", TopK: 3},
		WithSpanAttributes(
			metry.StringAttribute(OperationName, "override"),
			metry.StringAttribute(RetrievalProvider, "override"),
			metry.StringAttribute(RetrievalSource, "override"),
			metry.IntAttribute(RetrievalTopK, 99),
		),
	)
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "retrieval", testutil.SpanStubStringAttr(t, spans[0], OperationName))
	assert.Equal(t, "qdrant", testutil.SpanStubStringAttr(t, spans[0], RetrievalProvider))
	assert.Equal(t, "knowledge_base", testutil.SpanStubStringAttr(t, spans[0], RetrievalSource))
	assert.Equal(t, int64(3), testutil.SpanStubInt64Attr(t, spans[0], RetrievalTopK))
}
