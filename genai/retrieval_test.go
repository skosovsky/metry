package genai

import (
	"context"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/testutil"
)

func TestRetrievalResultRejectsUnboundedAndNonFiniteBeforeMutation(t *testing.T) {
	for _, result := range []RetrievalResult{
		{ReturnedDocuments: -1},
		{Documents: make([]RetrievalDocument, maxPayloadEntries+1)},
		{Documents: []RetrievalDocument{{Relevance: new(math.NaN())}}},
		{Documents: []RetrievalDocument{{Relevance: new(math.Inf(1))}}},
		{Documents: []RetrievalDocument{{Relevance: new(math.Inf(-1))}}},
	} {
		// Arrange.
		tracker, provider, exporter := newTestTracker(t)
		_, handle, startErr := tracker.StartRetrieval(
			context.Background(),
			"",
			RetrievalRequest{Locality: ExecutionLocal},
		)
		require.NoError(t, startErr)
		// Act.
		err := handle.Finish(result)
		require.NoError(t, handle.Close())
		flushTestProvider(t, provider)
		// Assert: validation precedes slice copy/attribute writes.
		require.ErrorIs(t, err, ErrInvalidObservation)
		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalReturnedDocuments))
		assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalDocuments))
	}
}

func TestStartRetrieval_SetsAttributesAndParent(t *testing.T) {
	tracker, provider, mem := newTestTracker(t,
		WithPayloadPolicy(RedactPayloadPolicy()),
		WithMaxContextLength(128),
	)

	parentCtx, endParent, err := provider.StartSpan(context.Background(), "genai-retrieval", "request")
	require.NoError(t, err)
	ctx, handle, startErr := tracker.StartRetrieval(
		parentCtx,
		"vector.search",
		RetrievalRequest{Locality: ExecutionLocal,
			Provider: "qdrant",
			Source:   "knowledge_base",
			Query:    strings.Repeat("a", 128),
			TopK:     5,
		},
	)
	require.NoError(t, startErr)
	require.NotNil(t, ctx)
	require.NoError(t, handle.Finish(RetrievalResult{
		Status:            OperationStatusOK,
		ReturnedDocuments: 0,
		Documents:         []RetrievalDocument{{Relevance: new(0.9)}, {Relevance: new(0.8)}},
	}))
	require.ErrorIs(t, handle.Close(), ErrFinalized)
	endParent()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	retrieval := testutil.SpanByName(t, spans, "retrieval "+fingerprintPayloadLabel("knowledge_base"))
	request := testutil.SpanByName(t, spans, "request")

	assert.Equal(t, request.SpanContext.SpanID(), retrieval.Parent.SpanID())
	assert.Equal(t, "retrieval", testutil.SpanStubStringAttr(t, retrieval, OperationName))
	assert.Equal(t, "qdrant", testutil.SpanStubStringAttr(t, retrieval, ProviderName))
	assert.Equal(
		t,
		fingerprintPayloadLabel("knowledge_base"),
		testutil.SpanStubStringAttr(t, retrieval, RetrievalSource),
	)
	assert.Equal(t, int64(5), testutil.SpanStubInt64Attr(t, retrieval, RetrievalTopK))

	query := testutil.SpanStubStringAttr(t, retrieval, RetrievalQuery)
	assert.LessOrEqual(t, len(query), 128)
	assert.True(t, utf8.ValidString(query))

	assert.Equal(t, int64(0), testutil.SpanStubInt64Attr(t, retrieval, RetrievalReturnedDocuments))
	assert.JSONEq(t, `[{"score":0.9},{"score":0.8}]`, testutil.SpanStubStringAttr(t, retrieval, RetrievalDocuments))
	testutil.AssertSpanStubOkStatus(t, retrieval)
}

func TestRetrievalFinish_SuccessSetsOkStatus(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	_, handle, startErr := tracker.StartRetrieval(
		context.Background(),
		"vector.search",
		RetrievalRequest{Locality: ExecutionLocal,
			Provider: "qdrant",
			TopK:     3,
		},
	)
	require.NoError(t, startErr)
	require.NoError(t, handle.Finish(RetrievalResult{Status: OperationStatusOK, ReturnedDocuments: 2}))
	require.ErrorIs(t, handle.Close(), ErrFinalized)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	testutil.AssertSpanStubOkStatus(t, spans[0])
}

func TestStartRetrieval_WithoutRecordPayloads_DoesNotSetQuery(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	_, handle, startErr := tracker.StartRetrieval(
		context.Background(),
		"vector.search",
		RetrievalRequest{Locality: ExecutionLocal,
			Provider: "qdrant",
			Query:    "sensitive",
			TopK:     5,
		},
	)
	require.NoError(t, startErr)
	require.NoError(t, handle.Close())

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalQuery))
}

func TestStartRetrieval_WithKeepHint_ExportsSpanWhenBaseSamplerDrops(t *testing.T) {
	tracker, provider, mem := newTestTrackerWithSampler(t, NewHintSampler(metry.NeverSample()))

	_, handle, startErr := tracker.StartRetrieval(
		context.Background(),
		"vector.search",
		RetrievalRequest{Locality: ExecutionLocal, Provider: "qdrant", TopK: 3},
		WithSpanSamplingKeep(),
	)
	require.NoError(t, startErr)
	require.NoError(t, handle.Close())

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "qdrant", testutil.SpanStubStringAttr(t, spans[0], ProviderName))
	assert.True(t, spans[0].SpanContext.IsSampled())
}

func TestStartRetrieval_WithCallerAttributes_PreservesBuiltInAttributes(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	_, handle, startErr := tracker.StartRetrieval(
		context.Background(),
		"vector.search",
		RetrievalRequest{Locality: ExecutionLocal, Provider: "qdrant", Source: "knowledge_base", TopK: 3},
		WithSpanAttributes(metry.StringAttribute("test.retrieval.caller", "present")),
	)
	require.NoError(t, startErr)
	require.NoError(t, handle.Close())

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "retrieval", testutil.SpanStubStringAttr(t, spans[0], OperationName))
	assert.Equal(t, "qdrant", testutil.SpanStubStringAttr(t, spans[0], ProviderName))
	assert.Equal(
		t,
		fingerprintPayloadLabel("knowledge_base"),
		testutil.SpanStubStringAttr(t, spans[0], RetrievalSource),
	)
	assert.Equal(t, "present", testutil.SpanStubStringAttr(t, spans[0], "test.retrieval.caller"))
}

func TestStartRetrieval_WithDuplicateCallerKeys_BuiltInWins(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	_, handle, startErr := tracker.StartRetrieval(
		context.Background(),
		"vector.search",
		RetrievalRequest{Locality: ExecutionLocal, Provider: "qdrant", Source: "knowledge_base", TopK: 3},
		WithSpanAttributes(
			metry.StringAttribute(OperationName, "override"),
			metry.StringAttribute(ProviderName, "override"),
			metry.StringAttribute(RetrievalSource, "override"),
			metry.IntAttribute(RetrievalTopK, 99),
		),
	)
	require.NoError(t, startErr)
	require.NoError(t, handle.Close())

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "retrieval", testutil.SpanStubStringAttr(t, spans[0], OperationName))
	assert.Equal(t, "qdrant", testutil.SpanStubStringAttr(t, spans[0], ProviderName))
	assert.Equal(
		t,
		fingerprintPayloadLabel("knowledge_base"),
		testutil.SpanStubStringAttr(t, spans[0], RetrievalSource),
	)
	assert.Equal(t, int64(3), testutil.SpanStubInt64Attr(t, spans[0], RetrievalTopK))
}
