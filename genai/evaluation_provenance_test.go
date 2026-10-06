package genai

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/metry/testutil"
)

func TestEvaluationIndependentProvenanceExport(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprint("raw=", raw), func(t *testing.T) {
			// Arrange: payload recording is disabled in the default profile.
			var options []Option
			if raw {
				options = append(options, WithRawPayloads())
			}
			tracker, provider, exports := newTestTracker(t, options...)
			refs := []EvaluationProvenance{
				{}, {GraderReference: "private-grader-digest"}, {TargetReference: "private-target-digest"},
				{
					GraderReference: "private-grader-digest",
					TargetReference: "private-target-digest",
				},
			}
			// Act: online and experiment membership are orthogonal.
			for _, experiment := range []bool{false, true} {
				for _, provenance := range refs {
					e := successfulEvaluation()
					e.Provenance = provenance
					if experiment {
						e.Association = EvaluationAssociation{
							ExperimentID:     "experiment",
							DatasetReference: "dataset",
							CaseID:           "case",
							TrialID:          "trial",
						}
					}
					require.NoError(t, NoopEvaluationRecorder().Record(t.Context(), e))
					require.NoError(t, tracker.EvaluationRecorder().Record(t.Context(), e))
				}
			}
			flushTestProvider(t, provider)
			// Assert: only supplied references appear, with no inferred experiment.
			spans := exports.GetSpans()
			require.Len(t, spans, 8)
			for i, span := range spans {
				assertProvenanceExport(t, span, refs[i%len(refs)], i >= len(refs), raw)
			}
			// Repeated references retain identity regardless of experiment membership.
			require.Equal(
				t,
				testutil.SpanStubStringAttr(t, spans[1], evaluationGraderKey),
				testutil.SpanStubStringAttr(t, spans[5], evaluationGraderKey),
			)
		})
	}
}

func TestEvaluationProvenanceBoundsAndPartialExperimentRejectBeforeExport(t *testing.T) {
	// Arrange.
	tracker, provider, exports := newTestTracker(t)
	invalid := []Evaluation{}
	for _, value := range []string{strings.Repeat("secret", maxIdentityBytes), string([]byte{0xff})} {
		for _, target := range []bool{false, true} {
			e := successfulEvaluation()
			if target {
				e.Provenance.TargetReference = value
			} else {
				e.Provenance.GraderReference = value
			}
			invalid = append(invalid, e)
		}
	}
	// Every nonempty proper subset of the four-field experiment tuple is invalid.
	for mask := 1; mask < 15; mask++ {
		e := successfulEvaluation()
		fields := []*string{
			&e.Association.ExperimentID,
			&e.Association.DatasetReference,
			&e.Association.CaseID,
			&e.Association.TrialID,
		}
		for i, p := range fields {
			if mask&(1<<i) != 0 {
				*p = "secret"
			}
		}
		invalid = append(invalid, e)
	}
	// Act and Assert: enabled/noop enforce the same validation without raw errors.
	for _, e := range invalid {
		for _, r := range []*EvaluationRecorder{tracker.EvaluationRecorder(), NoopEvaluationRecorder()} {
			err := r.Record(t.Context(), e)
			require.ErrorIs(t, err, ErrInvalidObservation)
			require.NotContains(t, err.Error(), "secret")
		}
	}
	flushTestProvider(t, provider)
	require.Empty(t, exports.GetSpans())
	// Exactly bounded UTF-8 references are accepted, independently.
	e := successfulEvaluation()
	e.Provenance = EvaluationProvenance{
		TargetReference: strings.Repeat("x", maxIdentityBytes),
		GraderReference: strings.Repeat("я", maxIdentityBytes/2),
	}
	require.NoError(t, NoopEvaluationRecorder().Record(t.Context(), e))
	require.NoError(t, tracker.EvaluationRecorder().Record(t.Context(), e))
	flushTestProvider(t, provider)
	require.Len(t, exports.GetSpans(), 1)
}

func assertProvenanceExport(t *testing.T, span tracetest.SpanStub, p EvaluationProvenance, experiment, raw bool) {
	t.Helper()
	require.Len(t, span.Events, 1)
	assertProvenanceReference(t, span, evaluationTargetKey, p.TargetReference, raw)
	assertProvenanceReference(t, span, evaluationGraderKey, p.GraderReference, raw)
	for _, key := range []string{evaluationExperimentKey, evaluationDatasetKey, evaluationCaseKey, evaluationTrialKey} {
		require.Equal(t, experiment, testutil.SpanStubHasAttr(span, key))
	}
	if !raw {
		require.NotContains(t, fmt.Sprint(span), "private-grader-digest")
		require.NotContains(t, fmt.Sprint(span), "private-target-digest")
	}
}

func assertProvenanceReference(t *testing.T, span tracetest.SpanStub, key, ref string, raw bool) {
	t.Helper()
	require.Equal(t, ref != "", testutil.SpanStubHasAttr(span, key))
	want := ref
	if ref != "" && !raw {
		want = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(ref)))
	}
	if ref != "" {
		require.Equal(t, want, testutil.SpanStubStringAttr(t, span, key))
	}
	found := false
	for _, a := range span.Events[0].Attributes {
		if string(a.Key) == key {
			found = true
			require.Equal(t, want, a.Value.AsString())
		}
	}
	require.Equal(t, ref != "", found)
}
