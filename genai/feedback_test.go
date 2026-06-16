package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestRecordAsyncFeedback_InvalidLinkedContext_ReturnsError(t *testing.T) {
	tracker, _, _ := newTestTracker(t)

	err := tracker.RecordAsyncFeedback(context.Background(), metry.AsyncHandle{}, 0.5, "bad")
	require.ErrorIs(t, err, ErrInvalidAsyncHandle)
}

func TestRecordAsyncFeedback_ValidRemoteLinked_AttachesSpanWithLink(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	linkedSC := testutil.NewTestParentSpanContext(true)
	linked := metrytest.AsyncHandleFromSpanContext(t, linkedSC)
	err := tracker.RecordAsyncFeedback(context.Background(), linked, 0.9, "should-not-export")
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "user_feedback", spans[0].Name)
	testutil.AssertLinkBasedAsyncSpan(t, spans[0], testutil.ProducerSpanStub(linkedSC))
	assert.InDelta(t, 0.9, testutil.SpanStubFloat64Attr(t, spans[0], EvaluationScore), 1e-9)
	assert.False(t, testutil.SpanStubHasAttr(spans[0], EvaluationText))
}

func TestRecordAsyncFeedback_WithPayloadRecording_SetsText(t *testing.T) {
	tracker, provider, mem := newTestTracker(t, WithRecordPayloads(true))

	linked := metrytest.AsyncHandleFromSpanContext(t, testutil.NewTestParentSpanContext(false))
	err := tracker.RecordAsyncFeedback(context.Background(), linked, 1.0, "approved")
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "approved", testutil.SpanStubStringAttr(t, spans[0], EvaluationText))
}

func TestRecordAsyncFeedback_IgnoresContextParent_UsesLinkOnly(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	ctx, endActive, err := provider.StartSpan(context.Background(), "feedback", "interaction")
	require.NoError(t, err)
	linkedSC := testutil.NewTestParentSpanContext(false)
	linked := metrytest.AsyncHandleFromSpanContext(t, linkedSC)
	endActive()

	err = tracker.RecordAsyncFeedback(ctx, linked, 0.7, "local-parent")
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	feedback := spans[1]
	assert.Equal(t, "user_feedback", feedback.Name)
	testutil.AssertLinkBasedAsyncSpan(t, feedback, testutil.ProducerSpanStub(linkedSC))
}

func TestRecordAsyncFeedback_WithKeepHint_ExportsSpanWhenBaseSamplerDrops(t *testing.T) {
	tracker, provider, mem := newTestTrackerWithSampler(t, NewHintSampler(metry.NeverSample()))

	linkedSC := testutil.NewUnsampledRemoteParentSpanContext()
	linked := metrytest.AsyncHandleFromSpanContext(t, linkedSC)
	err := tracker.RecordAsyncFeedback(
		context.Background(),
		linked,
		0.2,
		"negative",
		WithSamplingKeep(),
	)
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "user_feedback", spans[0].Name)
	testutil.AssertLinkBasedAsyncSpan(t, spans[0], testutil.ProducerSpanStub(linkedSC))
}

func TestRecordAsyncFeedback_WithoutKeepHint_DroppedWhenBaseSamplerDrops(t *testing.T) {
	tracker, provider, mem := newTestTrackerWithSampler(t, NewHintSampler(metry.NeverSample()))

	linked := metrytest.AsyncHandleFromSpanContext(t, testutil.NewUnsampledRemoteParentSpanContext())
	err := tracker.RecordAsyncFeedback(context.Background(), linked, 0.2, "negative")
	require.NoError(t, err)

	flushTestProvider(t, provider)
	require.Empty(t, mem.GetSpans())
}

func TestRecordAsyncFeedback_MetryAsyncHandle_RoundTrip(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	handle, originSC := metrytest.AsyncHandleFromProducerSpan(t, provider, mem, "producer", "enqueue")

	token, err := handle.Marshal()
	require.NoError(t, err)

	parsed, err := metry.ParseAsyncHandle(token)
	require.NoError(t, err)

	err = tracker.RecordAsyncFeedback(context.Background(), parsed, 0.9, "approved")
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	testutil.AssertLinkBasedAsyncSpan(t, spans[1], originSC)
	assert.Equal(t, "user_feedback", spans[1].Name)
}
