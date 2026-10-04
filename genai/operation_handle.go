package genai

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/genaiwire"
)

// ErrFinalized is returned when a local recording handle has already finished.
var ErrFinalized = errors.New("genai: recording already finalized")

// PredecessorState is supplied by the caller; sampling never implies export.
type PredecessorState string

const (
	PredecessorUnknown  PredecessorState = "unknown"
	PredecessorExported PredecessorState = "exported"
	PredecessorMissing  PredecessorState = "missing"
)

// RecordingOptions declare causal relations without owning execution lifecycle.
// Detached resume creates a new root linked to the current trace reference.
// Links and Identity are copied before recording. Noop paths validate these too.
type RecordingOptions struct {
	Identity    CausalIdentity
	Links       []metry.AsyncHandle
	Detached    bool
	Predecessor PredecessorState
	PolicyID    string
	Mapping     string
}

// OperationHandle records one local operation and accounts only its final usage.
// It owns no retries, scheduling, persistence or delivery. Invalid results can be
// corrected before Finish succeeds; successful finalization is irrevocable.
type OperationHandle struct {
	mu       sync.Mutex
	finished bool
	tracker  *Tracker
	ctx      context.Context
	span     trace.Span
	meta     Meta
	identity CausalIdentity
	started  time.Time
}

func (r trackerRuntime) StartOperation(
	ctx context.Context,
	operation Operation,
	options RecordingOptions,
) (context.Context, *OperationHandle, error) {
	return startOperation(ctx, r.tracker, r.runtimeProvider(), operation, options)
}

func (noopRuntime) StartOperation(
	ctx context.Context,
	operation Operation,
	options RecordingOptions,
) (context.Context, *OperationHandle, error) {
	return startOperation(ctx, nil, unknownProviderName, operation, options)
}

func startOperation(
	ctx context.Context,
	tracker *Tracker,
	provider string,
	operation Operation,
	options RecordingOptions,
) (context.Context, *OperationHandle, error) {
	if err := validateRecordingOptions(tracker, operation, options); err != nil {
		return ctx, nil, err
	}
	links, err := recordingLinks(ctx, options)
	if err != nil {
		return ctx, nil, err
	}
	// The DTO holds only strings: copying it freezes the caller's identity snapshot.
	meta := Meta{
		Provider:      provider,
		Operation:     operation.Name,
		Purpose:       operation.Purpose,
		RequestModel:  operation.Model,
		ResponseModel: "",
		ServerAddress: "",
		ServerPort:    0,
		Duration:      0,
		ErrorType:     "",
		Status:        "",
	}
	meta = mergeMetaFromScope(ctx, meta)
	started := time.Now()
	var span trace.Span
	if tracker != nil {
		attrs := append(buildMetaAttributes(meta, tracker.cfg), options.Identity.attributes(tracker.cfg)...)
		state := options.Predecessor
		if state == "" {
			state = PredecessorUnknown
		}
		attrs = append(attrs, attribute.String("metry.causal.predecessor", string(state)))
		spanOptions := []trace.SpanStartOption{
			trace.WithTimestamp(started),
			trace.WithAttributes(attrs...),
			trace.WithLinks(links...),
		}
		if options.Detached {
			spanOptions = append(spanOptions, trace.WithNewRoot())
		}
		ctx, span = tracker.tracer.Start(
			ctx,
			metadataWithConfig(OperationName, meta.Operation, tracker.cfg),
			spanOptions...)
	}
	handle := &OperationHandle{
		mu:       sync.Mutex{},
		finished: false,
		tracker:  tracker,
		ctx:      ctx,
		span:     span,
		meta:     meta,
		identity: options.Identity,
		started:  started,
	}
	return ctx, handle, nil
}

func validateRecordingOptions(tracker *Tracker, operation Operation, options RecordingOptions) error {
	if err := options.Identity.Validate(); err != nil {
		return err
	}
	if err := validateOperationName(operation.Name); err != nil {
		return err
	}
	if len(operation.Model) > maxIdentityBytes ||
		len(operation.Purpose) > maxMetricValueLength {
		return invalidObservation("operation", "malformed")
	}
	if len(options.Links) > maxCausalLinks {
		return invalidObservation("links", "too_large")
	}
	if options.Predecessor != "" && options.Predecessor != PredecessorUnknown &&
		options.Predecessor != PredecessorExported &&
		options.Predecessor != PredecessorMissing {
		return invalidObservation("predecessor", "invalid_state")
	}
	if options.Mapping != "" && options.Mapping != profileInclusiveMapping {
		return invalidObservation("mapping", "unsupported")
	}
	if options.PolicyID != "" {
		if tracker == nil {
			return invalidObservation("policy", "unresolved")
		}
		if err := tracker.CheckPolicy(options.PolicyID); err != nil {
			return err
		}
	}
	return nil
}

func recordingLinks(ctx context.Context, options RecordingOptions) ([]trace.Link, error) {
	links := make([]trace.Link, 0, len(options.Links)+1)
	if options.Detached {
		if origin := trace.SpanContextFromContext(ctx); origin.IsValid() {
			links = append(links, trace.Link{SpanContext: origin, Attributes: nil})
		}
	}
	for _, handle := range options.Links {
		spanContext, err := genaiwire.AsyncSpanContext(handle)
		if err != nil {
			return nil, err
		}
		duplicate := false
		for _, link := range links {
			if link.SpanContext.TraceID() == spanContext.TraceID() &&
				link.SpanContext.SpanID() == spanContext.SpanID() {
				duplicate = true
				break
			}
		}
		if !duplicate {
			links = append(links, trace.Link{SpanContext: spanContext, Attributes: nil})
		}
	}
	if len(links) > maxCausalLinks {
		return nil, invalidObservation("links", "too_large")
	}
	return links, nil
}

// Finish validates a final result before any outcome/usage writes. Concurrent
// successful finishes record once; later callers receive ErrFinalized. Business
// execution results remain the caller's responsibility even on telemetry error.
func (h *OperationHandle) Finish(result OperationResult) error {
	if h == nil || h.started.IsZero() {
		return invalidObservation("recording", "missing")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return ErrFinalized
	}
	status, class, outcomeErr := resolveModelOutcome(result.Status, result.ErrorType)
	if outcomeErr != nil {
		return outcomeErr
	}
	if result.Duration != 0 {
		return invalidObservation("duration", "measured_locally")
	}
	if h.tracker != nil {
		if err := h.tracker.validateUsage(result.Usage); err != nil {
			return err
		}
	} else {
		if err := result.Usage.Validate(); err != nil {
			return err
		}
	}
	finished := time.Now()
	meta := h.meta
	meta.Duration = finished.Sub(h.started)
	if result.Streaming != nil {
		if err := validateStreamTimings(*result.Streaming, meta.Duration); err != nil {
			return err
		}
	}
	if h.tracker == nil {
		h.finished = true
		return nil
	}
	payloadAttrs, err := buildPayloadAttributes(result.Payload, h.tracker.cfg)
	if err != nil {
		return err
	}
	h.finished = true
	meta.Status = Outcome(status)
	meta.ErrorType = class
	meta.ResponseModel = result.Response.Model
	h.span.SetAttributes(attribute.String(OperationStatus, status))
	h.tracker.recordInteractionOnSpan(
		h.ctx,
		h.span,
		meta,
		payloadAttrs,
		result.Usage,
		h.identity.Kind != IdentityLogical,
	)
	if result.Streaming != nil && h.identity.Kind != IdentityLogical {
		h.tracker.recordStreamingQuality(h.ctx, meta, result.Usage.OutputTokens, *result.Streaming, meta.Duration)
	}
	h.span.End(trace.WithTimestamp(finished))
	return nil
}

// RecordStreamingUpdate validates an intermediate cumulative observation without
// adding it to metrics. Only Finish commits usage/cost; metry never estimates a
// missing final total from partial updates.
func (h *OperationHandle) RecordStreamingUpdate(usage Usage) error {
	if h == nil || h.started.IsZero() {
		return invalidObservation("recording", "missing")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return ErrFinalized
	}
	if h.tracker != nil {
		if err := h.tracker.validateUsage(usage); err != nil {
			return err
		}
	} else {
		if err := usage.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Close ends the observation with unknown outcome and no inferred resources.
func (h *OperationHandle) Close() error {
	if h == nil || h.started.IsZero() {
		return invalidObservation("recording", "missing")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return ErrFinalized
	}
	h.finished = true
	if h.tracker == nil {
		return nil
	}
	meta := h.meta
	meta.Status = OperationStatusUnset
	meta.ErrorType = ""
	meta.ResponseModel = ""
	meta.Duration = time.Since(h.started)
	var usage Usage
	h.tracker.recordInteractionOnSpan(h.ctx, h.span, meta, nil, usage, h.identity.Kind != IdentityLogical)
	h.span.End()
	return nil
}
