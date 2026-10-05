package genai

import (
	"context"
	"math"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/genaiwire"
	"github.com/skosovsky/metry/internal/traceutil"
)

const maxEvaluationTextBytes = 1 << 20

// EvaluationMetric is a typed machine-evaluation metric name.
type EvaluationMetric string

const (
	EvaluationFaithfulness       EvaluationMetric = "faithfulness"
	EvaluationAnswerRelevance    EvaluationMetric = "answer_relevance"
	EvaluationContextPrecision   EvaluationMetric = "context_precision"
	EvaluationHallucinationIndex EvaluationMetric = "hallucination_index"
)

// EvaluationStatus distinguishes a result from a failed/absent measurement.
type EvaluationStatus string

const (
	EvaluationSucceeded EvaluationStatus = "succeeded"
	EvaluationFailed    EvaluationStatus = "failed"
	EvaluationSkipped   EvaluationStatus = "skipped"
)

// EvaluationAssociation is optional outside experiment mode. In experiment
// mode all fields are required. References are opaque identities/content digests;
// metry never fetches datasets, targets, graders or artifacts.
type EvaluationAssociation struct {
	ExperimentID     string
	DatasetReference string
	CaseID           string
	TrialID          string
	TargetReference  string
	GraderReference  string
}

// Evaluation is a completed result, never an instruction to run a grader.
// Succeeded contains exactly one of Score or Outcome. Failed/skipped contain
// neither. Score permits a known zero and has no globally imposed range.
// TraceReference nil means standalone; non-nil invalid references are errors.
// IDs/references are fingerprinted unless raw mode is explicitly permitted,
// retaining their equality across repeated deliveries without exposing values.
type Evaluation struct {
	ObservationID     string
	Metric            EvaluationMetric
	Status            EvaluationStatus
	Score             *float64
	Outcome           string
	Reasoning         string
	Association       EvaluationAssociation
	TraceReference    *metry.AsyncHandle
	ArtifactReference string
}

// Validate rejects invalid forms before any instrumentation side effect.
func (e Evaluation) Validate() error {
	if e.ObservationID == "" || !boundedIdentity(e.ObservationID) ||
		e.Metric == "" || !boundedIdentity(string(e.Metric)) {
		return invalidObservation(profileEvaluationOperation, "missing_or_malformed_identity")
	}
	if len(e.Reasoning) > maxEvaluationTextBytes || len(e.Outcome) > maxIdentityBytes ||
		!boundedIdentity(
			e.ArtifactReference,
		) || !utf8.ValidString(e.Reasoning) || !utf8.ValidString(e.Outcome) {
		return invalidObservation(profileEvaluationOperation, "too_large_or_malformed")
	}
	switch e.Status {
	case EvaluationSucceeded:
		if (e.Score == nil) == (e.Outcome == "") {
			return invalidObservation("evaluation_result", "exactly_one_required")
		}
		if e.Score != nil && (math.IsNaN(*e.Score) || math.IsInf(*e.Score, 0)) {
			return invalidObservation("evaluation_score", "non_finite")
		}
	case EvaluationFailed, EvaluationSkipped:
		if e.Score != nil || e.Outcome != "" {
			return invalidObservation("evaluation_result", "unexpected_value")
		}
	default:
		return invalidObservation("evaluation_status", "unsupported")
	}
	if err := e.Association.validate(); err != nil {
		return err
	}
	if e.TraceReference != nil && !e.TraceReference.IsValid() {
		return invalidObservation("evaluation_trace", "invalid_reference")
	}
	return nil
}

func (a EvaluationAssociation) validate() error {
	values := []string{
		a.ExperimentID,
		a.DatasetReference,
		a.CaseID,
		a.TrialID,
		a.TargetReference,
		a.GraderReference,
	}
	populated := 0
	for _, value := range values {
		if value != "" {
			populated++
		}
		if !boundedIdentity(value) {
			return invalidObservation("evaluation_association", "malformed")
		}
	}
	if populated != 0 && populated != len(values) {
		return invalidObservation("evaluation_association", "incomplete_experiment")
	}
	return nil
}

// EvaluationRecorder records ready results with the tracker's provider/policy.
// It owns no execution, storage, ordering or delivery registry.
type EvaluationRecorder struct{ tracker *Tracker }

// EvaluationRecorder returns an app-scoped recorder sharing this configuration.
func (t *Tracker) EvaluationRecorder() *EvaluationRecorder {
	return &EvaluationRecorder{tracker: t}
}

// NoopEvaluationRecorder validates contracts without exporting observations.
func NoopEvaluationRecorder() *EvaluationRecorder {
	return &EvaluationRecorder{tracker: nil}
}

// Record emits a standalone or linked root and never mutates the target span.
// Repeated delivery retains identity but creates another observation.
func (r *EvaluationRecorder) Record(ctx context.Context, evaluation Evaluation) error {
	if err := evaluation.Validate(); err != nil {
		return err
	}
	if r == nil || r.tracker == nil {
		return nil
	}
	options := []trace.SpanStartOption{trace.WithNewRoot()}
	if evaluation.TraceReference != nil {
		spanContext, err := genaiwire.AsyncSpanContext(*evaluation.TraceReference)
		if err != nil {
			return invalidObservation("evaluation_trace", "invalid_reference")
		}
		options = append(
			options,
			trace.WithLinks(trace.Link{SpanContext: spanContext, Attributes: nil}),
		)
	}
	attrs := r.attributes(evaluation)
	options = append(options, trace.WithAttributes(attrs...))
	_, span := r.tracker.tracer.Start(ctx, profileEvaluationOperation, options...)
	defer span.End()
	span.AddEvent("gen_ai.evaluation.result", trace.WithAttributes(attrs...))
	traceutil.SpanOK(span)
	return nil
}

func (r *EvaluationRecorder) attributes(e Evaluation) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(OperationName, profileEvaluationOperation),
		attribute.String(OperationPurpose, PurposeQualityEvaluation),
		attribute.String(EvaluationMetricName, evaluationMetricWithConfig(e.Metric, r.tracker.cfg)),
		attribute.String(evaluationStatusKey, string(e.Status)),
		attribute.String(evaluationObservationKey, r.reference(e.ObservationID)),
	}
	if e.Score != nil {
		attrs = append(attrs, attribute.Float64(EvaluationScore, *e.Score))
	}
	for _, item := range []struct{ key, value string }{
		{EvaluationLabel, e.Outcome},
		{evaluationExperimentKey, e.Association.ExperimentID},
		{evaluationDatasetKey, e.Association.DatasetReference},
		{evaluationCaseKey, e.Association.CaseID},
		{evaluationTrialKey, e.Association.TrialID},
		{evaluationTargetKey, e.Association.TargetReference},
		{evaluationGraderKey, e.Association.GraderReference},
		{evaluationArtifactKey, e.ArtifactReference},
	} {
		if item.value != "" {
			attrs = append(attrs, attribute.String(item.key, r.reference(item.value)))
		}
	}
	if r.tracker.cfg.RecordPayloads() && e.Reasoning != "" {
		attrs = append(attrs, attribute.String(
			EvaluationExplanation,
			truncateContextWithLimit(
				sanitizeTextWithConfig(e.Reasoning, r.tracker.cfg),
				r.tracker.cfg.MaxEventLength(),
			),
		))
	}
	return attrs
}

func evaluationMetricWithConfig(metric EvaluationMetric, cfg runtimeConfig) string {
	switch metric {
	case EvaluationFaithfulness, EvaluationAnswerRelevance, EvaluationContextPrecision, EvaluationHallucinationIndex:
		return string(metric)
	default:
		return metadataWithConfig(EvaluationMetricName, string(metric), cfg)
	}
}

func (r *EvaluationRecorder) reference(value string) string {
	return referenceWithConfig(value, r.tracker.cfg)
}
