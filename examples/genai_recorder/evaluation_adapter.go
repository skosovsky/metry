package main

import (
	"errors"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
)

// readyGrade is the host's projection of an already returned evaly-style Grade.
// For scored grades the host selects one Metrics entry per observation; for
// failed/skipped grades it supplies the checked metric's identity, not a score.
// GraderReference identifies the full caller-owned grader revision; target is
// optional in Assess-only flows. Neither reference requires an experiment.
// This example imports no evaly types and performs no grading or target work.
type readyGrade struct {
	ObservationID   string
	Metric          genai.EvaluationMetric
	Status          string
	Value           float64
	GraderReference string
	TargetReference string
	Reasoning       string
}

func evaluationFromReadyGrade(
	grade readyGrade,
	origin *metry.AsyncHandle,
) (genai.Evaluation, error) {
	observation := genai.Evaluation{
		ObservationID: grade.ObservationID,
		Metric:        grade.Metric,
		Reasoning:     grade.Reasoning,
		Provenance: genai.EvaluationProvenance{
			GraderReference: grade.GraderReference,
			TargetReference: grade.TargetReference,
		},
		TraceReference: origin,
	}
	switch grade.Status {
	case "scored":
		observation.Status = genai.EvaluationSucceeded
		observation.Score = &grade.Value
	case "grader_error":
		observation.Status = genai.EvaluationFailed
	case "not_applicable", "insufficient_evidence":
		observation.Status = genai.EvaluationSkipped
	default:
		return genai.Evaluation{}, errors.New("unsupported ready grade status")
	}
	if err := observation.Validate(); err != nil {
		return genai.Evaluation{}, err
	}
	return observation, nil
}
