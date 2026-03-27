package genai

import (
	"context"

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
}

// StartRetrievalSpan starts a child span for retrieval I/O and writes request attributes.
// Extra start options allow callers to add start-time sampling hints or attributes.
//
//nolint:spancheck // The span is returned to the caller, which is responsible for ending it.
func (t *Tracker) StartRetrievalSpan(
	ctx context.Context,
	name string,
	req RetrievalRequest,
	startOpts ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	spanName := name
	if spanName == "" {
		spanName = "retrieval"
	}

	attrs := make([]attribute.KeyValue, 0, retrievalStartAttrCap)
	attrs = append(attrs, OperationNameKey.String("retrieval"))
	if req.Provider != "" {
		attrs = append(attrs, RetrievalProviderKey.String(req.Provider))
	}
	if req.Source != "" {
		attrs = append(attrs, RetrievalSourceKey.String(req.Source))
	}
	if req.TopK > 0 {
		attrs = append(attrs, RetrievalTopKKey.Int(req.TopK))
	}
	if t.cfg.RecordPayloads() && req.Query != "" {
		attrs = append(attrs, RetrievalQueryKey.String(truncateContextWithConfig(req.Query, t.cfg)))
	}

	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	opts = append(opts, startOpts...)
	ctx, span := t.tracer.Start(ctx, spanName, opts...)
	// Preserve deterministic helper semantics when caller start options use duplicate keys.
	span.SetAttributes(attrs...)
	return ctx, span
}

// RecordRetrievalResult writes retrieval result attributes on the retrieval span.
func (t *Tracker) RecordRetrievalResult(span trace.Span, result RetrievalResult) {
	attrs := []attribute.KeyValue{
		RetrievalReturnedChunksKey.Int(result.ReturnedChunks),
	}
	if len(result.Distances) > 0 {
		attrs = append(attrs, RetrievalDistancesKey.Float64Slice(append([]float64(nil), result.Distances...)))
	}
	span.SetAttributes(attrs...)
}
