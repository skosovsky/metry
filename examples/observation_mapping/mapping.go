package main

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
)

const deltaMode = "delta"

var errMapping = errors.New("host: unsupported or contradictory observation")

// reading is the producer's own measurement contract, not a telemetry DTO.
type reading[T int | float64] struct {
	value  T
	known  bool
	source string
	unit   string
}

type hostUsage struct {
	input, output, cacheRead, cacheWrite, reasoning reading[int]
	cost                                            reading[float64]
	currency                                        string
}

func reported[T int | float64](value T, unit, source string) reading[T] {
	return reading[T]{value: value, known: true, unit: unit, source: source}
}

func mapReading[T int | float64](r reading[T]) (genai.Measurement[T], error) {
	if !r.known {
		if r.value != 0 || r.unit != "" || (r.source != "" && r.source != "unknown") {
			return genai.Measurement[T]{}, errMapping
		}
		return genai.Measurement[T]{}, nil
	}
	var source genai.Source
	switch r.source {
	case "observed":
		source = genai.SourceMeasured
	case "reported":
		source = genai.SourceProvider
	case "estimated":
		source = genai.SourceEstimated
	default:
		return genai.Measurement[T]{}, errMapping
	}
	var unit genai.Unit
	switch r.unit {
	case "tokens":
		unit = genai.UnitTokens
	case "money":
		unit = genai.UnitCurrency
	default:
		return genai.Measurement[T]{}, errMapping
	}
	return genai.Observe(r.value, unit, source), nil
}

func mapUsage(input hostUsage) (genai.Usage, error) {
	var output genai.Usage
	for _, pair := range []struct {
		input  reading[int]
		output *genai.Measurement[int]
	}{
		{input.input, &output.InputTokens}, {input.output, &output.OutputTokens},
		{input.cacheRead, &output.CacheReadInputTokens}, {input.cacheWrite, &output.CacheWriteInputTokens},
		{input.reasoning, &output.ReasoningOutputTokens},
	} {
		mapped, err := mapReading(pair.input)
		if err != nil {
			return genai.Usage{}, err
		}
		*pair.output = mapped
	}
	cost, err := mapReading(input.cost)
	if err != nil {
		return genai.Usage{}, err
	}
	output.Cost, output.Currency = cost, input.currency
	if err := output.Validate(); err != nil {
		return genai.Usage{}, err
	}
	return output, nil
}

// These are host types, deliberately independent of the producer's Go types.
type modelResult struct {
	state         string
	domainSuccess *bool
	usage         hostUsage
	response      string
	err           error
}

type mappedModel struct {
	result     genai.OperationResult
	attributes []metry.Attribute
}

func mapModel(input modelResult) (mappedModel, error) {
	var status genai.Outcome
	var class string
	switch input.state {
	case "completed", "refusal", "incomplete":
		status = genai.OperationStatusOK
	case "failed":
		status, class = genai.OperationStatusError, string(genai.ErrorClassUnknown)
	case "cancelled":
		status, class = genai.OperationStatusError, string(genai.ErrorClassCancelled)
	case "deadline":
		status, class = genai.OperationStatusError, string(genai.ErrorClassDeadlineExceeded)
	case "unknown":
		status = genai.OperationStatusUnset
	default:
		return mappedModel{}, errMapping
	}
	if input.err != nil {
		status = genai.OperationStatusError
		// Explicit cancellation/deadline state is already bounded evidence.
		if class == "" || class == string(genai.ErrorClassUnknown) {
			class = string(genai.ErrorClassUnknown)
			switch {
			case errors.Is(input.err, context.Canceled):
				class = string(genai.ErrorClassCancelled)
			case errors.Is(input.err, context.DeadlineExceeded):
				class = string(genai.ErrorClassDeadlineExceeded)
			}
		}
	}
	usage, err := mapUsage(input.usage)
	if err != nil {
		return mappedModel{}, err
	}
	result := genai.OperationResult{Status: status, ErrorType: class, Usage: usage}
	if input.response != "" {
		result.Payload.OutputMessages = []genai.Message{
			{Role: "assistant", Parts: []genai.ContentPart{{Type: "text", Content: input.response}}},
		}
	}
	attrs := []metry.Attribute{metry.StringAttribute("host.model.provider_outcome", input.state)}
	if input.domainSuccess != nil {
		attrs = append(attrs, metry.BoolAttribute("host.model.domain_success", *input.domainSuccess))
	}
	return mappedModel{result: result, attributes: attrs}, nil
}

type usageUpdate struct {
	sequence int
	mode     string
	tokens   int
	terminal bool
}

// normalizeUpdates is host protocol logic, not a core accumulator. This example
// producer reports output tokens only; a real producer maps its other fields too.
func normalizeUpdates(updates []usageUpdate) (hostUsage, error) {
	if len(updates) == 0 || !updates[len(updates)-1].terminal {
		return hostUsage{}, errMapping
	}
	total := 0
	mode := updates[0].mode
	for i, u := range updates {
		if u.sequence != i+1 || u.mode != mode || u.tokens < 0 || (u.terminal && i != len(updates)-1) {
			return hostUsage{}, errMapping
		}
		switch mode {
		case deltaMode:
			if u.tokens > math.MaxInt-total {
				return hostUsage{}, errMapping
			}
			total += u.tokens
		case "cumulative":
			if u.tokens < total {
				return hostUsage{}, errMapping
			}
			total = u.tokens
		default:
			return hostUsage{}, errMapping
		}
	}
	return hostUsage{output: reported(total, "tokens", "reported")}, nil
}

// Ownership is decided at host wiring time, not inferred from trace ancestry.
func checkOwner(owners []string) error {
	if len(owners) != 1 || owners[0] == "" {
		return errMapping
	}
	return nil
}

type hostAssociation struct{ experiment, dataset, caseID, trial string }

type readyAssessment struct {
	id          string
	metric      string
	state       string
	score       float64
	assertion   bool
	reasoning   string
	association hostAssociation
	target      string
	grader      string
}

func mapAssessment(input readyAssessment, origin *metry.AsyncHandle) (genai.Evaluation, error) {
	result := genai.Evaluation{
		ObservationID: input.id,
		Metric:        genai.EvaluationMetric(input.metric),
		Reasoning:     input.reasoning,
		Association: genai.EvaluationAssociation{
			ExperimentID:     input.association.experiment,
			DatasetReference: input.association.dataset,
			CaseID:           input.association.caseID,
			TrialID:          input.association.trial,
		},
		Provenance:     genai.EvaluationProvenance{TargetReference: input.target, GraderReference: input.grader},
		TraceReference: origin,
	}
	switch input.state {
	case "scored":
		result.Status, result.Score = genai.EvaluationSucceeded, &input.score
	case "assertion":
		result.Status, result.Outcome = genai.EvaluationSucceeded, "false"
		if input.assertion {
			result.Outcome = "true"
		}
	case "unavailable", "grader_error":
		result.Status = genai.EvaluationFailed
	case "skipped":
		result.Status = genai.EvaluationSkipped
	default:
		return genai.Evaluation{}, errMapping
	}
	if err := result.Validate(); err != nil {
		return genai.Evaluation{}, err
	}
	return result, nil
}

// measurementSink illustrates an idempotent host storage protocol. It is not an
// exporter, durable outbox or a way to deduplicate calls to EvaluationRecorder.
type measurementSink struct {
	mu           sync.Mutex
	measurements map[string]genai.Evaluation
}

func (s *measurementSink) accept(e genai.Evaluation, loseAcknowledgement bool) error {
	if err := e.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, ok := s.measurements[e.ObservationID]; ok && !reflect.DeepEqual(previous, e) {
		return errMapping
	}
	if s.measurements == nil {
		s.measurements = make(map[string]genai.Evaluation)
	}
	// Copy caller-owned pointers so mutation cannot rewrite the committed record.
	if e.Score != nil {
		value := *e.Score
		e.Score = &value
	}
	if e.TraceReference != nil {
		value := *e.TraceReference
		e.TraceReference = &value
	}
	s.measurements[e.ObservationID] = e
	if loseAcknowledgement {
		return errors.New("host: acknowledgement lost after commit")
	}
	return nil
}
