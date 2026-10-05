package metry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// ContextHandler is a [slog.Handler] decorator that injects enrich attributes and trace correlation fields.
type ContextHandler struct {
	slog.Handler

	reserved map[string]bool
}

// Handle adds business attributes from Enrich baggage and W3C trace/span ids before delegating to the base handler.
func (h ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	recordKeys := make(map[string]bool)
	r.Attrs(func(attr slog.Attr) bool { recordKeys[attr.Key] = true; return true })
	for _, attr := range slogAttrsFromBaggage(ctx) {
		if !attr.local && (h.reserved[attr.key] || recordKeys[attr.key]) {
			continue
		}
		r.AddAttrs(attributeToSlogAttr(attr.key, attr.value, attr.attrType))
	}

	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", spanCtx.TraceID().String()),
			slog.String("span_id", spanCtx.SpanID().String()),
		)
	}

	return h.Handler.Handle(ctx, r)
}

// WithAttrs returns a handler that prepends attrs to each record (delegates to base handler).
func (h ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	keys := make(map[string]bool, len(h.reserved)+len(attrs))
	for key := range h.reserved {
		keys[key] = true
	}
	for _, attr := range attrs {
		keys[attr.Key] = true
	}
	return ContextHandler{Handler: h.Handler.WithAttrs(attrs), reserved: keys}
}

// WithGroup returns a handler scoped to a group name (delegates to base handler).
func (h ContextHandler) WithGroup(name string) slog.Handler {
	return ContextHandler{Handler: h.Handler.WithGroup(name), reserved: h.reserved}
}

// Enabled reports whether the handler handles records at the given level.
func (h ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}
