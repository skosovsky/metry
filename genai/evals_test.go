package genai

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestRecordEvaluations_InvalidLinkedContext_ReturnsError(t *testing.T) {
	tracker, _, _ := newTestTracker(t)

	err := tracker.RecordEvaluations(context.Background(), metry.AsyncHandle{}, []Evaluation{
		{Metric: EvaluationFaithfulness, Score: 0.8},
	})
	require.ErrorIs(t, err, ErrInvalidAsyncHandle)
}

func TestRecordEvaluations_ValidLinked_AddsSpanWithLinkAndEvents(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	linkedSC := testutil.NewTestParentSpanContext(true)
	handle := metrytest.AsyncHandleFromSpanContext(t, linkedSC)
	err := tracker.RecordEvaluations(context.Background(), handle, []Evaluation{
		{Metric: EvaluationFaithfulness, Score: 0.91, Reasoning: "hidden"},
		{Metric: EvaluationAnswerRelevance, Score: 0.76},
	})
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "llm_evaluations", spans[0].Name)
	testutil.AssertLinkBasedAsyncSpan(t, spans[0], testutil.ProducerSpanStub(linkedSC))

	assert.Equal(t, PurposeQualityEvaluation, testutil.SpanStubStringAttr(t, spans[0], OperationPurpose))

	require.Len(t, spans[0].Events, 2)
	assert.Equal(t, "evaluation", spans[0].Events[0].Name)
	assert.Equal(t, "evaluation", spans[0].Events[1].Name)

	firstEvent := testutil.AttrsStub(spans[0].Events[0].Attributes)
	assert.Equal(t, string(EvaluationFaithfulness), testutil.SpanStubStringAttr(t, firstEvent, EvaluationMetricName))
	assert.InDelta(t, 0.91, testutil.SpanStubFloat64Attr(t, firstEvent, EvaluationScore), 1e-9)
	assert.False(t, testutil.SpanStubHasAttr(firstEvent, EvaluationReasoning))
}

func TestRecordEvaluations_WithPayloadRecording_IncludesReasoning(t *testing.T) {
	tracker, provider, mem := newTestTracker(t,
		WithRecordPayloads(true),
		WithMaxContextLength(1<<20), // span payload limit; event reasoning uses MaxEventLength instead
		WithMaxEventLength(48),
	)

	handle := metrytest.AsyncHandleFromSpanContext(t, testutil.NewTestParentSpanContext(false))
	err := tracker.RecordEvaluations(context.Background(), handle, []Evaluation{
		{
			Metric:    EvaluationContextPrecision,
			Score:     0.67,
			Reasoning: strings.Repeat("x", 200),
		},
	})
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)

	event := testutil.AttrsStub(spans[0].Events[0].Attributes)
	reasoning := testutil.SpanStubStringAttr(t, event, EvaluationReasoning)
	assert.LessOrEqual(t, len(reasoning), 48)
	assert.True(t, utf8.ValidString(reasoning))
}

func TestRecordEvaluations_ReasoningTruncatedToDefaultMaxEventLength(t *testing.T) {
	tracker, provider, mem := newTestTracker(t, WithRecordPayloads(true))

	// defaultMaxEventLength (4096) = rune-safe prefix + len(truncationSuffix) (15 bytes).
	require.Len(t, truncationSuffix, 15)

	handle := metrytest.AsyncHandleFromSpanContext(t, testutil.NewTestParentSpanContext(false))
	err := tracker.RecordEvaluations(context.Background(), handle, []Evaluation{
		{
			Metric:    EvaluationFaithfulness,
			Score:     0.5,
			Reasoning: strings.Repeat("a", 12000),
		},
	})
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)

	event := testutil.AttrsStub(spans[0].Events[0].Attributes)
	reasoning := testutil.SpanStubStringAttr(t, event, EvaluationReasoning)
	assert.Len(t, reasoning, defaultMaxEventLength)
	assert.True(t, strings.HasSuffix(reasoning, truncationSuffix))
	assert.True(t, utf8.ValidString(reasoning))
}

func TestRecordEvaluations_ReasoningTruncated_UTF8RuneBoundary(t *testing.T) {
	const eventLimit = 64
	tracker, provider, mem := newTestTracker(t,
		WithRecordPayloads(true),
		WithMaxEventLength(eventLimit),
	)

	// bodyBudget is the max bytes kept before the truncation suffix; place a 3-byte UTF-8 rune (世) so that
	// byte index bodyBudget falls inside that rune and truncateAtRuneBoundary must back up to a rune start.
	bodyBudget := eventLimit - len(truncationSuffix)
	const threeByteUTF8RuneLen = 3 // e.g. U+4E16 世
	prefixLen := bodyBudget - (threeByteUTF8RuneLen - 1)
	prefix := strings.Repeat("b", prefixLen)
	reasoning := prefix + "世" + strings.Repeat("c", 100)

	handle := metrytest.AsyncHandleFromSpanContext(t, testutil.NewTestParentSpanContext(false))
	err := tracker.RecordEvaluations(context.Background(), handle, []Evaluation{
		{Metric: EvaluationFaithfulness, Score: 0.1, Reasoning: reasoning},
	})
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	event := testutil.AttrsStub(spans[0].Events[0].Attributes)
	out := testutil.SpanStubStringAttr(t, event, EvaluationReasoning)
	assert.LessOrEqual(t, len(out), eventLimit)
	assert.True(t, strings.HasSuffix(out, truncationSuffix))
	assert.True(t, utf8.ValidString(out))
}

func TestRecordEvaluations_WithKeepHint_ExportsSpanWhenBaseSamplerDrops(t *testing.T) {
	tracker, provider, mem := newTestTrackerWithSampler(t, NewHintSampler(metry.NeverSample()))

	linkedSC := testutil.NewUnsampledRemoteParentSpanContext()
	handle := metrytest.AsyncHandleFromSpanContext(t, linkedSC)
	err := tracker.RecordEvaluations(
		context.Background(),
		handle,
		[]Evaluation{{Metric: EvaluationFaithfulness, Score: 0.2}},
		WithSamplingKeep(),
	)
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "llm_evaluations", spans[0].Name)
	testutil.AssertLinkBasedAsyncSpan(t, spans[0], testutil.ProducerSpanStub(linkedSC))
}

func TestRecordEvaluations_WithoutKeepHint_DroppedWhenBaseSamplerDrops(t *testing.T) {
	tracker, provider, mem := newTestTrackerWithSampler(t, NewHintSampler(metry.NeverSample()))

	handle := metrytest.AsyncHandleFromSpanContext(t, testutil.NewUnsampledRemoteParentSpanContext())
	err := tracker.RecordEvaluations(
		context.Background(),
		handle,
		[]Evaluation{{Metric: EvaluationFaithfulness, Score: 0.2}},
	)
	require.NoError(t, err)

	flushTestProvider(t, provider)
	require.Empty(t, mem.GetSpans())
}

func TestRecordEvaluations_EmptySlice_ReturnsErrNoEvaluations(t *testing.T) {
	tracker, _, _ := newTestTracker(t)

	handle := metrytest.AsyncHandleFromSpanContext(t, testutil.NewTestParentSpanContext(true))
	err := tracker.RecordEvaluations(context.Background(), handle, nil)
	require.ErrorIs(t, err, ErrNoEvaluations)
}

func TestRecordEvaluations_MetryAsyncHandle_RoundTrip(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	handle, originSC := metrytest.AsyncHandleFromProducerSpan(t, provider, mem, "producer", "enqueue")

	token, err := handle.Marshal()
	require.NoError(t, err)

	parsed, err := metry.ParseAsyncHandle(token)
	require.NoError(t, err)

	err = tracker.RecordEvaluations(context.Background(), parsed, []Evaluation{
		{Metric: EvaluationFaithfulness, Score: 0.85},
	})
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	testutil.AssertLinkBasedAsyncSpan(t, spans[1], originSC)
	assert.Equal(t, "llm_evaluations", spans[1].Name)
}
