package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/genai"
)

func TestReadyGradeAdapterPreservesPresenceAndIndependentProvenance(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   genai.EvaluationStatus
		score  bool
	}{
		{"scored", genai.EvaluationSucceeded, true},
		{"grader_error", genai.EvaluationFailed, false},
		{"not_applicable", genai.EvaluationSkipped, false},
		{"insufficient_evidence", genai.EvaluationSkipped, false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			// Arrange: Assess can identify its grader without a target or experiment.
			grade := readyGrade{
				ObservationID:   "observation",
				Metric:          genai.EvaluationFaithfulness,
				Status:          tc.status,
				Value:           0,
				GraderReference: "grader-revision-digest",
			}
			// Act.
			observation, err := evaluationFromReadyGrade(grade, nil)
			// Assert: failures/skip never become a measured zero.
			require.NoError(t, err)
			require.Equal(t, tc.want, observation.Status)
			require.Equal(t, tc.score, observation.Score != nil)
			if tc.score {
				require.Zero(t, *observation.Score)
			}
			require.Equal(t, genai.EvaluationAssociation{}, observation.Association)
			require.Equal(t, grade.GraderReference, observation.Provenance.GraderReference)
			require.Empty(t, observation.Provenance.TargetReference)
		})
	}
}

func TestReadyGradeAdapterRejectsUnknownState(t *testing.T) {
	// Arrange.
	grade := readyGrade{
		ObservationID: "observation",
		Metric:        genai.EvaluationFaithfulness,
		Status:        "unknown",
	}
	// Act.
	observation, err := evaluationFromReadyGrade(grade, nil)
	// Assert.
	require.Error(t, err)
	require.Equal(t, genai.Evaluation{}, observation)
}
