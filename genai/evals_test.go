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
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func successfulEvaluation() Evaluation {
	return Evaluation{ObservationID: "observation", Metric: EvaluationFaithfulness,
		Status: EvaluationSucceeded, Outcome: "pass"}
}

func TestEvaluationInvalidFormsRejectBeforeExportIncludingNoop(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t)
	zero, nan, infinity := 0.0, math.NaN(), math.Inf(1)
	invalid := []Evaluation{
		{},
		{ObservationID: "secret", Metric: EvaluationFaithfulness, Status: EvaluationSucceeded},
		{
			ObservationID: "secret",
			Metric:        EvaluationFaithfulness,
			Status:        EvaluationSucceeded,
			Score:         &zero,
			Outcome:       "pass",
		},
		{
			ObservationID: "secret",
			Metric:        EvaluationFaithfulness,
			Status:        EvaluationSucceeded,
			Score:         &nan,
		},
		{
			ObservationID: "secret",
			Metric:        EvaluationFaithfulness,
			Status:        EvaluationSucceeded,
			Score:         &infinity,
		},
		{
			ObservationID: "secret",
			Metric:        EvaluationFaithfulness,
			Status:        EvaluationFailed,
			Score:         &zero,
		},
		{
			ObservationID: "secret",
			Metric:        EvaluationFaithfulness,
			Status:        EvaluationSkipped,
			Outcome:       "pass",
		},
		{ObservationID: "secret", Metric: EvaluationFaithfulness, Status: "secret"},
	}
	partial := successfulEvaluation()
	partial.Association.TrialID = "secret"
	invalid = append(invalid, partial)
	badTrace := successfulEvaluation()
	badTrace.TraceReference = &metry.AsyncHandle{}
	invalid = append(invalid, badTrace)
	// Act and Assert.
	for _, evaluation := range invalid {
		for _, recorder := range []*EvaluationRecorder{tracker.EvaluationRecorder(), NoopEvaluationRecorder()} {
			err := recorder.Record(context.Background(), evaluation)
			require.ErrorIs(t, err, ErrInvalidObservation)
			assert.NotContains(t, err.Error(), "secret")
		}
	}
	flushTestProvider(t, provider)
	assert.Empty(t, exporter.GetSpans())
}

func TestEvaluationStandaloneZeroCategoryFailedSkipped(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t)
	zero := 0.0
	base := successfulEvaluation()
	scored := base
	scored.Outcome, scored.Score = "", &zero
	failed, skipped := base, base
	failed.Status, failed.Outcome = EvaluationFailed, ""
	skipped.Status, skipped.Outcome = EvaluationSkipped, ""
	// Act.
	for _, evaluation := range []Evaluation{scored, base, failed, skipped} {
		require.NoError(t, tracker.EvaluationRecorder().Record(context.Background(), evaluation))
	}
	flushTestProvider(t, provider)
	// Assert: failures/skip are not score zero; nil trace creates standalone roots.
	spans := exporter.GetSpans()
	require.Len(t, spans, 4)
	for _, span := range spans {
		assert.False(t, span.Parent.IsValid())
		assert.Empty(t, span.Links)
		require.Len(t, span.Events, 1)
	}
	assert.InDelta(t, 0, testutil.SpanStubFloat64Attr(t, spans[0], EvaluationScore), 1e-9)
	assert.False(t, testutil.SpanStubHasAttr(spans[1], EvaluationScore))
	assert.False(t, testutil.SpanStubHasAttr(spans[2], EvaluationScore))
	assert.False(t, testutil.SpanStubHasAttr(spans[3], EvaluationScore))
	assert.Equal(
		t,
		string(EvaluationFailed),
		testutil.SpanStubStringAttr(t, spans[2], "metry.evaluation.status"),
	)
}

func TestEvaluationTrialsReferencesAndRepeatDelivery(t *testing.T) {
	// Arrange: raw opt-in makes exact caller identities observable in this fixture.
	tracker, provider, exporter := newTestTracker(t, WithRawPayloads())
	evaluation := successfulEvaluation()
	evaluation.Association = EvaluationAssociation{
		ExperimentID: "experiment", DatasetReference: "dataset-digest", CaseID: "case",
		TrialID: "trial-one", TargetReference: "target-digest", GraderReference: "grader-digest",
	}
	evaluation.ArtifactReference = "opaque-artifact-digest"
	// Act: repeated delivery is not a deduplication promise.
	require.NoError(t, tracker.EvaluationRecorder().Record(context.Background(), evaluation))
	require.NoError(t, tracker.EvaluationRecorder().Record(context.Background(), evaluation))
	evaluation.Association.TrialID = "trial-two"
	evaluation.Association.TargetReference = "changed-target"
	require.NoError(t, tracker.EvaluationRecorder().Record(context.Background(), evaluation))
	flushTestProvider(t, provider)
	// Assert.
	spans := exporter.GetSpans()
	require.Len(t, spans, 3)
	for _, span := range spans {
		assert.Equal(
			t,
			"observation",
			testutil.SpanStubStringAttr(t, span, "metry.evaluation.observation_id"),
		)
		assert.Equal(
			t,
			"opaque-artifact-digest",
			testutil.SpanStubStringAttr(t, span, "metry.evaluation.artifact_reference"),
		)
	}
	assert.Equal(
		t,
		"trial-one",
		testutil.SpanStubStringAttr(t, spans[0], "metry.evaluation.trial_id"),
	)
	assert.Equal(
		t,
		"trial-two",
		testutil.SpanStubStringAttr(t, spans[2], "metry.evaluation.trial_id"),
	)
	assert.Equal(
		t,
		"changed-target",
		testutil.SpanStubStringAttr(t, spans[2], "metry.evaluation.target_reference"),
	)
}

func TestEvaluationLinkedToUnsampledTargetWithoutMutatingIt(t *testing.T) {
	// Arrange: a valid reference does not prove target export/execution absence.
	tracker, provider, exporter := newTestTracker(t)
	origin := testutil.NewUnsampledRemoteParentSpanContext()
	handle := metrytest.AsyncHandleFromSpanContext(t, origin)
	token, err := handle.Marshal()
	require.NoError(t, err)
	parsed, err := metry.ParseAsyncHandle(token)
	require.NoError(t, err)
	evaluation := successfulEvaluation()
	evaluation.TraceReference = &parsed
	// Act.
	require.NoError(t, tracker.EvaluationRecorder().Record(context.Background(), evaluation))
	flushTestProvider(t, provider)
	// Assert: a new linked root is exported, not an update of the unsampled target.
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	testutil.AssertLinkBasedAsyncSpan(t, spans[0], testutil.ProducerSpanStub(origin))
}

func TestEvaluationPrivacyAndBoundedReasoning(t *testing.T) {
	for _, policy := range []PayloadPolicy{RedactPayloadPolicy(), HashPayloadPolicy()} {
		// Arrange.
		tracker, provider, exporter := newTestTracker(
			t,
			WithPayloadPolicy(policy),
			WithMaxEventLength(64),
		)
		evaluation := successfulEvaluation()
		evaluation.ObservationID = "secret-marker"
		evaluation.Outcome = "secret-marker"
		evaluation.ArtifactReference = "secret-marker"
		evaluation.Reasoning = strings.Repeat("secret-marker", 1000)
		// Act.
		require.NoError(t, tracker.EvaluationRecorder().Record(context.Background(), evaluation))
		require.NoError(t, tracker.EvaluationRecorder().Record(context.Background(), evaluation))
		flushTestProvider(t, provider)
		// Assert: stable fingerprint association, raw reasoning never escapes.
		spans := exporter.GetSpans()
		require.Len(t, spans, 2)
		assert.Equal(t, testutil.SpanStubStringAttr(t, spans[0], "metry.evaluation.observation_id"),
			testutil.SpanStubStringAttr(t, spans[1], "metry.evaluation.observation_id"))
		for _, span := range spans {
			assert.NotContains(t, span.Attributes, "secret-marker")
			for _, attr := range span.Attributes {
				assert.NotContains(t, attr.Value.AsString(), "secret-marker")
			}
			reasoning := testutil.SpanStubStringAttr(t, span, EvaluationExplanation)
			assert.LessOrEqual(t, len(reasoning), 64)
			assert.True(t, utf8.ValidString(reasoning))
		}
	}
}

func TestEvaluationHonorsDropSampling(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTrackerWithSampler(t, metry.NeverSample())
	// Act.
	require.NoError(
		t,
		tracker.EvaluationRecorder().Record(context.Background(), successfulEvaluation()),
	)
	flushTestProvider(t, provider)
	// Assert.
	assert.Empty(t, exporter.GetSpans())
}

func TestEvaluationRawReasoningLimitsAndCallerDefinedScoreScale(t *testing.T) {
	for _, limit := range []int{64, defaultMaxEventLength} {
		// Arrange: the first rune at the cutoff may span several bytes.
		tracker, provider, exporter := newTestTracker(
			t,
			WithRawPayloads(),
			WithMaxEventLength(limit),
		)
		evaluation := successfulEvaluation()
		score := -12.5 // The metric contract, not metry, defines a valid score range.
		evaluation.Outcome, evaluation.Score = "", &score
		evaluation.Reasoning = strings.Repeat("世", defaultMaxEventLength)
		// Act.
		require.NoError(t, tracker.EvaluationRecorder().Record(context.Background(), evaluation))
		flushTestProvider(t, provider)
		// Assert.
		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.InDelta(t, score, testutil.SpanStubFloat64Attr(t, spans[0], EvaluationScore), 1e-9)
		reasoning := testutil.SpanStubStringAttr(t, spans[0], EvaluationExplanation)
		assert.LessOrEqual(t, len(reasoning), limit)
		assert.True(t, strings.HasSuffix(reasoning, truncationSuffix))
		assert.True(t, utf8.ValidString(reasoning))
	}
}
