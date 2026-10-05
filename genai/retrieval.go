package genai

import (
	"context"
	"sync"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const retrievalStartAttrCap = 5

// RetrievalRequest describes one retrieval operation at span start.
type RetrievalRequest struct {
	Locality ExecutionLocality
	Provider string
	Source   string
	Query    string
	TopK     int
}

// RetrievalDocument contains caller-owned optional opaque provenance, never document content.
// Relevance is absent when nil. Any finite score is allowed, including known zero.
type RetrievalDocument struct {
	ID        string
	Version   string
	Digest    string
	Relevance *float64
}

// RetrievalResult contains caller facts; Documents can be a partial provenance subset.
// ReturnedDocuments is not inferred from optional documents or relevance scores.
type RetrievalResult struct {
	ReturnedDocuments int
	Documents         []RetrievalDocument
	Status            Outcome
	Err               error
	ErrorClass        ErrorClass
}

// RetrievalHandle owns one caller-observed local or remote retrieval interval.
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
	if err := validateRetrievalStart(name, req); err != nil {
		return ctx, nil, err
	}
	handle := &RetrievalHandle{mu: sync.Mutex{}, finished: false, tracker: t, startedAt: time.Now(), span: nil}
	if t == nil {
		return ctx, handle, nil
	}
	spanName := profileRetrievalOperation
	if req.Source != "" {
		spanName += " " + referenceWithConfig(req.Source, t.cfg)
	}

	attrs := make([]attribute.KeyValue, 0, retrievalStartAttrCap)
	attrs = append(attrs, attribute.Key(OperationName).String(profileRetrievalOperation))
	if name != "" {
		attrs = append(attrs, attribute.String(RetrievalName, metadataWithConfig(RetrievalName, name, t.cfg)))
	}
	if req.Provider != "" {
		attrs = append(
			attrs,
			attribute.Key(ProviderName).String(metadataWithConfig(ProviderName, req.Provider, t.cfg)),
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
	opts = append(opts, trace.WithSpanKind(req.Locality.spanKind()))
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
	documents, err := prepareRetrievalDocuments(result)
	if err != nil {
		return err
	}
	status, errorType, err := resolveToolOutcome(h.tracker, result.Status, result.Err, result.ErrorClass)
	if err != nil {
		return err
	}
	var encoded string
	var truncated bool
	if h.tracker != nil && len(documents) > 0 {
		protectRetrievalDocuments(documents, h.tracker.cfg)
		encoded, truncated, err = boundedJSONArray(documents, h.tracker.cfg.MaxContextLength())
		if err != nil {
			return err
		}
	}
	h.finished = true
	if h.span != nil {
		attrs := []attribute.KeyValue{attribute.Int(RetrievalReturnedDocuments, result.ReturnedDocuments)}
		if encoded != "" {
			attrs = append(attrs, attribute.String(RetrievalDocuments, encoded))
		}
		if truncated {
			attrs = append(attrs, attribute.Bool(RetrievalDocumentsTruncated, true))
		}
		h.span.SetAttributes(attrs...)
		applyOutcome(h.span, status, errorType)
		h.span.End()
	}
	return nil
}

// Close closes an observation without claiming a result or returned documents.
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

func validateRetrievalStart(name string, req RetrievalRequest) error {
	for _, value := range []string{name, req.Provider, req.Source} {
		if !boundedRetrievalReference(value) {
			return invalidObservation("retrieval", "input_bounds")
		}
	}
	if len(req.Query) > maxPayloadInputBytes || !utf8.ValidString(req.Query) || req.TopK < 0 {
		return invalidObservation("retrieval", "input_bounds")
	}
	return req.Locality.Validate()
}
