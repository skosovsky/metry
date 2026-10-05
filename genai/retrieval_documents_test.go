package genai

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/testutil"
)

func TestRetrievalDocumentsPrivacyWithContentDisabledAndScorePresence(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t)
	docs := []RetrievalDocument{
		{ID: "secret document", Version: "secret version", Digest: "secret digest", Relevance: nil},
		{Relevance: new(0.0)}, {Relevance: new(-2.5)}, {Relevance: new(10.0)},
	}
	_, handle, err := tracker.StartRetrieval(
		context.Background(),
		"",
		RetrievalRequest{Locality: ExecutionRemote, Source: "secret source", Query: "secret query"},
	)
	require.NoError(t, err)
	// Act.
	require.NoError(t, handle.Finish(RetrievalResult{ReturnedDocuments: 19, Documents: docs}))
	*docs[1].Relevance = 98
	docs[0].ID = "mutated"
	flushTestProvider(t, provider)
	// Assert.
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, trace.SpanKindClient, span.SpanKind)
	assert.Equal(t, "retrieval "+fingerprintPayloadLabel("secret source"), span.Name)
	assert.False(t, testutil.SpanStubHasAttr(span, RetrievalQuery))
	assert.Equal(t, int64(19), testutil.SpanStubInt64Attr(t, span, RetrievalReturnedDocuments))
	encoded := testutil.SpanStubStringAttr(t, span, RetrievalDocuments)
	assert.NotContains(t, encoded, "secret")
	var wire []retrievalDocumentWire
	require.NoError(t, json.Unmarshal([]byte(encoded), &wire))
	require.Len(t, wire, 4)
	assert.Equal(t, fingerprintPayloadLabel("secret document"), wire[0].ID)
	assert.Equal(t, fingerprintPayloadLabel("secret version"), wire[0].Version)
	assert.Equal(t, fingerprintPayloadLabel("secret digest"), wire[0].Digest)
	assert.Nil(t, wire[0].Score)
	require.NotNil(t, wire[1].Score)
	assert.InDelta(t, 0.0, *wire[1].Score, 0)
	assert.InDelta(t, -2.5, *wire[2].Score, 0)
	assert.InDelta(t, 10.0, *wire[3].Score, 0)
}

func TestRetrievalDocumentsRawReferencePolicyIndependentFromContentFlag(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(
		t,
		WithRawPayloads(),
		func(cfg *runtimeConfig) { cfg.recordPayloads = false },
	)
	_, handle, err := tracker.StartRetrieval(
		context.Background(),
		"",
		RetrievalRequest{Locality: ExecutionLocal, Query: "hidden query"},
	)
	require.NoError(t, err)
	// Act.
	require.NoError(
		t,
		handle.Finish(
			RetrievalResult{
				Documents: []RetrievalDocument{{ID: "raw document", Version: "v 1", Digest: "hash opaque"}},
			},
		),
	)
	flushTestProvider(t, provider)
	// Assert.
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalQuery))
	assert.JSONEq(
		t,
		`[{"id":"raw document","metry.document.version":"v 1","metry.document.digest":"hash opaque"}]`,
		testutil.SpanStubStringAttr(t, spans[0], RetrievalDocuments),
	)
}

func TestRetrievalDocumentsBoundsRejectBeforeWritesAndRemainCorrectable(t *testing.T) {
	invalid := []RetrievalResult{
		{ReturnedDocuments: -1},
		{Documents: make([]RetrievalDocument, maxPayloadEntries+1)},
		{Documents: []RetrievalDocument{{ID: strings.Repeat("x", maxIdentityBytes+1)}}},
		{Documents: []RetrievalDocument{{Version: string([]byte{0xff})}}},
		{Documents: []RetrievalDocument{{Digest: strings.Repeat("x", maxIdentityBytes+1)}}},
		{Documents: []RetrievalDocument{{Relevance: new(math.NaN())}}},
		{Documents: []RetrievalDocument{{Relevance: new(math.Inf(1))}}},
		{Documents: []RetrievalDocument{{Relevance: new(math.Inf(-1))}}},
	}
	nodes := make([]RetrievalDocument, 2048)
	for index := range nodes {
		nodes[index] = RetrievalDocument{ID: "id"}
	}
	invalid = append(invalid, RetrievalResult{Documents: nodes})
	bytes := make([]RetrievalDocument, 1300)
	for index := range bytes {
		bytes[index] = RetrievalDocument{
			ID:      strings.Repeat("<", 256),
			Version: strings.Repeat("<", 256),
			Digest:  strings.Repeat("<", 256),
		}
	}
	invalid = append(invalid, RetrievalResult{Documents: bytes})
	for _, result := range invalid {
		// Arrange.
		tracker, provider, exporter := newTestTracker(t)
		_, handle, err := tracker.StartRetrieval(context.Background(), "", RetrievalRequest{Locality: ExecutionLocal})
		require.NoError(t, err)
		_, noop, err := NoopRuntime().StartRetrieval(context.Background(), "", RetrievalRequest{Locality: ExecutionLocal})
		require.NoError(t, err)
		// Act.
		require.ErrorIs(t, handle.Finish(result), ErrInvalidObservation)
		require.ErrorIs(t, noop.Finish(result), ErrInvalidObservation)
		require.NoError(t, handle.Close())
		require.NoError(t, noop.Finish(RetrievalResult{}))
		flushTestProvider(t, provider)
		// Assert.
		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalDocuments))
		assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalReturnedDocuments))
		assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalDocumentsTruncated))
	}
}

func TestRetrievalDocumentsSchemaPreservingPrefixTruncation(t *testing.T) {
	for _, budget := range []int{len(truncatedJSONMarker), 20, 64} {
		// Arrange.
		tracker, provider, exporter := newTestTracker(t, WithRawPayloads(), WithMaxContextLength(budget))
		_, handle, err := tracker.StartRetrieval(context.Background(), "", RetrievalRequest{Locality: ExecutionLocal})
		require.NoError(t, err)
		docs := []RetrievalDocument{{ID: strings.Repeat("a", 40), Relevance: new(0.0)}, {ID: "second"}}
		// Act.
		require.NoError(t, handle.Finish(RetrievalResult{ReturnedDocuments: 12, Documents: docs}))
		flushTestProvider(t, provider)
		// Assert.
		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		value := testutil.SpanStubStringAttr(t, spans[0], RetrievalDocuments)
		require.LessOrEqual(t, len(value), budget)
		var actual []retrievalDocumentWire
		require.NoError(t, json.Unmarshal([]byte(value), &actual))
		require.Less(t, len(actual), len(docs))
		assert.True(t, testutil.SpanStubBoolAttr(t, spans[0], RetrievalDocumentsTruncated))
		assert.Equal(t, int64(12), testutil.SpanStubInt64Attr(t, spans[0], RetrievalReturnedDocuments))
	}
}

func TestRetrievalLocalityAndMalformedStartRejectedBeforeRecording(t *testing.T) {
	for _, request := range []RetrievalRequest{
		{}, {Locality: "unknown"}, {Locality: ExecutionLocal, Source: string([]byte{0xff})},
		{Locality: ExecutionRemote, Query: string([]byte{0xff})},
	} {
		// Arrange.
		tracker, provider, exporter := newTestTracker(t)
		// Act.
		_, handle, err := tracker.StartRetrieval(context.Background(), "", request)
		_, noop, noopErr := NoopRuntime().StartRetrieval(context.Background(), "", request)
		flushTestProvider(t, provider)
		// Assert.
		require.ErrorIs(t, err, ErrInvalidObservation)
		require.ErrorIs(t, noopErr, ErrInvalidObservation)
		assert.Nil(t, handle)
		assert.Nil(t, noop)
		assert.Empty(t, exporter.GetSpans())
	}
}

func TestRetrievalDoesNotGenerateDocumentIDsOrRequireProvenance(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t)
	_, handle, err := tracker.StartRetrieval(
		context.Background(),
		"",
		RetrievalRequest{Locality: ExecutionLocal},
		WithSpanAttributes(metry.StringAttribute("caller", "allowed")),
	)
	require.NoError(t, err)
	// Act.
	require.NoError(t, handle.Finish(RetrievalResult{ReturnedDocuments: 0, Status: OperationStatusOK}))
	require.ErrorIs(t, handle.Finish(RetrievalResult{ReturnedDocuments: -1}), ErrFinalized)
	flushTestProvider(t, provider)
	// Assert.
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "retrieval", spans[0].Name)
	assert.Equal(t, trace.SpanKindInternal, spans[0].SpanKind)
	assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalDocuments))
	assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalDocumentsTruncated))
}

func TestRetrievalDocumentInputAcceptsExactGlobalNodeBudget(t *testing.T) {
	// Arrange: array plus 1365 objects with two scalar properties = 4096 JSON nodes.
	documents := make([]RetrievalDocument, (maxPayloadEntries-1)/3)
	for index := range documents {
		documents[index] = RetrievalDocument{ID: "id", Relevance: new(0.0)}
	}
	tracker, provider, exporter := newTestTracker(t, WithRawPayloads(), WithMaxContextLength(maxPayloadInputBytes))
	_, handle, err := tracker.StartRetrieval(context.Background(), "", RetrievalRequest{Locality: ExecutionLocal})
	require.NoError(t, err)
	// Act.
	require.NoError(t, handle.Finish(RetrievalResult{Documents: documents}))
	flushTestProvider(t, provider)
	// Assert.
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	var observed []retrievalDocumentWire
	require.NoError(t, json.Unmarshal([]byte(testutil.SpanStubStringAttr(t, spans[0], RetrievalDocuments)), &observed))
	require.Len(t, observed, len(documents))
	assert.False(t, testutil.SpanStubHasAttr(spans[0], RetrievalDocumentsTruncated))
}
