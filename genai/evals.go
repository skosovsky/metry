package genai

import (
	"context"
	"errors"

	"github.com/skosovsky/metry"
)

const evaluationEventAttrCap = 3

// ErrNoEvaluations is returned when RecordEvaluations is called with an empty slice.
var ErrNoEvaluations = errors.New("genai: no evaluations provided")

// EvaluationMetric is a typed machine-evaluation metric name.
type EvaluationMetric string

const (
	EvaluationFaithfulness       EvaluationMetric = "faithfulness"
	EvaluationAnswerRelevance    EvaluationMetric = "answer_relevance"
	EvaluationContextPrecision   EvaluationMetric = "context_precision"
	EvaluationHallucinationIndex EvaluationMetric = "hallucination_index"
)

// Evaluation is one LLM-judge evaluation result.
type Evaluation struct {
	Metric    EvaluationMetric
	Score     float64
	Reasoning string
}

// RecordEvaluations records LLM-judge evaluations on a new span linked to the original interaction.
func (t *Tracker) RecordEvaluations(
	ctx context.Context,
	handle metry.AsyncHandle,
	evaluations []Evaluation,
	opts ...LinkedSpanOption,
) error {
	if !handle.IsValid() {
		return ErrInvalidAsyncHandle
	}
	if len(evaluations) == 0 {
		return ErrNoEvaluations
	}

	spanOpts := append([]LinkedSpanOption{WithLinkedPurpose(PurposeQualityEvaluation)}, opts...)

	return handle.RecordLinkedSpan(ctx, t.provider, "llm_evaluations", func(w metry.LinkedSpanWriter) error {
		for _, evaluation := range evaluations {
			attrs := make([]metry.Attribute, 0, evaluationEventAttrCap)
			attrs = append(attrs, metry.FloatAttribute(EvaluationScore, evaluation.Score))
			if evaluation.Metric != "" {
				attrs = append(attrs, metry.StringAttribute(EvaluationMetricName, string(evaluation.Metric)))
			}
			if t.cfg.RecordPayloads() && evaluation.Reasoning != "" {
				attrs = append(attrs, metry.StringAttribute(
					EvaluationReasoning,
					truncateContextWithLimit(evaluation.Reasoning, t.cfg.MaxEventLength()),
				))
			}
			w.AddEvent("evaluation", attrs...)
		}
		return nil
	}, spanOpts...)
}
