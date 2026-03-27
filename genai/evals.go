package genai

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const evaluationEventAttrCap = 3

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

// RecordEvaluations records LLM-judge evaluations on a child span.
func (t *Tracker) RecordEvaluations(
	ctx context.Context,
	parent trace.SpanContext,
	evaluations []Evaluation,
	opts ...trace.SpanStartOption,
) error {
	if !parent.IsValid() {
		return ErrParentSpanContextRequired
	}
	if len(evaluations) == 0 {
		return nil
	}

	if parent.IsRemote() {
		ctx = trace.ContextWithRemoteSpanContext(ctx, parent)
	} else {
		ctx = trace.ContextWithSpanContext(ctx, parent)
	}

	startOpts := []trace.SpanStartOption{
		trace.WithAttributes(OperationPurposeKey.String(PurposeQualityEvaluation)),
	}
	startOpts = append(startOpts, opts...)
	_, span := t.tracer.Start(ctx, "llm_evaluations", startOpts...)
	// Preserve deterministic helper semantics when caller start options use duplicate keys.
	span.SetAttributes(OperationPurposeKey.String(PurposeQualityEvaluation))
	defer span.End()

	for _, evaluation := range evaluations {
		attrs := make([]attribute.KeyValue, 0, evaluationEventAttrCap)
		attrs = append(attrs, EvaluationScoreKey.Float64(evaluation.Score))
		if evaluation.Metric != "" {
			attrs = append(attrs, EvaluationMetricNameKey.String(string(evaluation.Metric)))
		}
		if t.cfg.RecordPayloads() && evaluation.Reasoning != "" {
			attrs = append(attrs, EvaluationReasoningKey.String(
				truncateContextWithLimit(evaluation.Reasoning, t.cfg.MaxEventLength()),
			))
		}
		span.AddEvent("evaluation", trace.WithAttributes(attrs...))
	}

	return nil
}
