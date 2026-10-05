package genai

import (
	"context"
	"math"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const retrievalStartAttrCap = 5

// RetrievalRequest describes one retrieval operation at span start.
type RetrievalRequest struct {
	Provider string
	Source   string
	Query    string
	TopK     int
}

// RetrievalResult describes retrieval outputs available after I/O completes.
type RetrievalResult struct {
	ReturnedChunks int
	Distances      []float64
	Status         Outcome
	Err            error
	ErrorClass     ErrorClass
}

// RetrievalHandle owns one local retrieval observation.
type RetrievalHandle struct {
	mu        sync.Mutex
	finished  bool
	startedAt time.Time
	tracker   *Tracker
	span      trace.Span
}

// StartRetrieval begins telemetry before caller-owned retrieval I/O.
func (t *Tracker) StartRetrieval(
	ctx context.Context,
	name string,
	req RetrievalRequest,
	opts ...ChildSpanOption,
) (context.Context, *RetrievalHandle, error) {
	return startRetrieval(ctx, t, name, req, opts...)
}

func startRetrieval(
	ctx context.Context,
	t *Tracker,
	name string,
	req RetrievalRequest,
	startOpts ...ChildSpanOption,
) (context.Context, *RetrievalHandle, error) {
	if len(name) > maxIdentityBytes || len(req.Provider) > maxIdentityBytes || len(req.Source) > maxIdentityBytes ||
		len(req.Query) > maxPayloadInputBytes || req.TopK < 0 {
		return ctx, nil, invalidObservation("retrieval", "input_bounds")
	}
	handle := &RetrievalHandle{mu: sync.Mutex{}, finished: false, tracker: t, startedAt: time.Now(), span: nil}
	if t == nil {
		return ctx, handle, nil
	}
	spanName := name
	if spanName == "" {
		spanName = profileRetrievalOperation
	}
	spanName = metadataWithConfig(OperationName, spanName, t.cfg)

	attrs := make([]attribute.KeyValue, 0, retrievalStartAttrCap)
	attrs = append(attrs, attribute.Key(OperationName).String(profileRetrievalOperation))
	if req.Provider != "" {
		attrs = append(
			attrs,
			attribute.Key(RetrievalProvider).String(metadataWithConfig(ProviderName, req.Provider, t.cfg)),
		)
	}
	if req.Source != "" {
		attrs = append(attrs, attribute.Key(RetrievalSource).String(referenceWithConfig(req.Source, t.cfg)))
	}
	if req.TopK > 0 {
		attrs = append(attrs, attribute.Key(RetrievalTopK).Int(req.TopK))
	}
	if t.cfg.RecordPayloads() && req.Query != "" {
		query := sanitizeTextWithConfig(req.Query, t.cfg)
		attrs = append(attrs, attribute.Key(RetrievalQuery).String(truncateContextWithConfig(query, t.cfg)))
	}

	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...), trace.WithTimestamp(handle.startedAt)}
	opts = append(opts, childSpanOptionsToOTel(startOpts...)...)
	ctx, span := t.tracer.Start(ctx, spanName, opts...) //nolint:spancheck // handle owns finalization
	// Preserve deterministic helper semantics when caller start options use duplicate keys.
	span.SetAttributes(attrs...)
	handle.span = span
	return ctx, handle, nil //nolint:spancheck // handle owns finalization
}

// Finish validates before committing an outcome; failed validation is correctable.
func (h *RetrievalHandle) Finish(result RetrievalResult) error {
	if h == nil || h.startedAt.IsZero() {
		return invalidObservation("recording", "missing")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return ErrFinalized
	}
	if result.ReturnedChunks < 0 || len(result.Distances) > maxPayloadEntries {
		return invalidObservation("retrieval_result", "input_bounds")
	}
	for _, distance := range result.Distances {
		if math.IsNaN(distance) || math.IsInf(distance, 0) {
			return invalidObservation("retrieval_distance", "non_finite")
		}
	}
	status, errorType, err := resolveToolOutcome(h.tracker, result.Status, result.Err, result.ErrorClass)
	if err != nil {
		return err
	}
	h.finished = true
	if h.span != nil {
		attrs := []attribute.KeyValue{attribute.Int(RetrievalReturnedChunks, result.ReturnedChunks)}
		if len(result.Distances) > 0 {
			attrs = append(
				attrs,
				attribute.Float64Slice(RetrievalDistances, append([]float64(nil), result.Distances...)),
			)
		}
		h.span.SetAttributes(attrs...)
		applyOutcome(h.span, status, errorType)
		h.span.End()
	}
	return nil
}

// Close closes an observation without claiming a result or returned chunks.
func (h *RetrievalHandle) Close() error {
	if h == nil || h.startedAt.IsZero() {
		return invalidObservation("recording", "missing")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return ErrFinalized
	}
	h.finished = true
	if h.span != nil {
		applyOutcome(h.span, OperationStatusUnset, "")
		h.span.End()
	}
	return nil
}
