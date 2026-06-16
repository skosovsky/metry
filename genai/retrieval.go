package genai

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"
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

// StartRetrievalSpan starts a child span for retrieval I/O and returns an end callback.
// Call RecordRetrievalResult before end() for explicit I/O completion semantics.
// The end callback sets Ok when span status is still Unset.
// Extra start options allow callers to add start-time sampling hints or attributes.
func (t *Tracker) StartRetrievalSpan(
	ctx context.Context,
	name string,
	req RetrievalRequest,
	startOpts ...ChildSpanOption,
) (context.Context, func()) {
	spanName := name
	if spanName == "" {
		spanName = "retrieval"
	}

	attrs := make([]attribute.KeyValue, 0, retrievalStartAttrCap)
	attrs = append(attrs, attribute.Key(OperationName).String("retrieval"))
	if req.Provider != "" {
		attrs = append(attrs, attribute.Key(RetrievalProvider).String(req.Provider))
	}
	if req.Source != "" {
		attrs = append(attrs, attribute.Key(RetrievalSource).String(req.Source))
	}
	if req.TopK > 0 {
		attrs = append(attrs, attribute.Key(RetrievalTopK).Int(req.TopK))
	}
	if t.cfg.RecordPayloads() && req.Query != "" {
		attrs = append(attrs, attribute.Key(RetrievalQuery).String(truncateContextWithConfig(req.Query, t.cfg)))
	}

	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	opts = append(opts, childSpanOptionsToOTel(startOpts...)...)
	ctx, span := t.tracer.Start(ctx, spanName, opts...) //nolint:spancheck // caller ends span via callback
	// Preserve deterministic helper semantics when caller start options use duplicate keys.
	span.SetAttributes(attrs...)
	return ctx, func() { traceutil.EndSpanOKIfUnset(span) } //nolint:spancheck // caller ends span via callback
}

// RecordRetrievalResult writes retrieval result attributes on the span stored in ctx.
func (t *Tracker) RecordRetrievalResult(ctx context.Context, result RetrievalResult) {
	mutateSpan(ctx, func(span trace.Span) {
		attrs := []attribute.KeyValue{
			attribute.Key(RetrievalReturnedChunks).Int(result.ReturnedChunks),
		}
		if len(result.Distances) > 0 {
			distances := append([]float64(nil), result.Distances...)
			attrs = append(attrs, attribute.Key(RetrievalDistances).Float64Slice(distances))
		}
		span.SetAttributes(attrs...)
		traceutil.SpanOK(span)
	})
}
