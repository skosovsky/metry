package async

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"
)

// StartLinkedSpan starts a new root span linked to the handle origin.
// The caller must call span.End().
func StartLinkedSpan(
	ctx context.Context,
	tracer trace.Tracer,
	h Handle,
	name string,
	opts ...trace.SpanStartOption,
) (context.Context, trace.Span, error) {
	if tracer == nil {
		return ctx, nil, ErrNilTracer
	}
	if !h.IsValid() {
		return ctx, nil, ErrInvalidHandle
	}
	startOpts := []trace.SpanStartOption{
		trace.WithNewRoot(),
		trace.WithLinks(trace.Link{SpanContext: h.spanContext, Attributes: nil}),
	}
	startOpts = append(startOpts, opts...)
	ctx, span := tracer.Start(ctx, name, startOpts...) //nolint:spancheck // caller ends span
	return ctx, span, nil                              //nolint:spancheck // caller ends span
}

// RunLinkedSpan starts a linked root span, runs fn, and ends the span.
func (h Handle) RunLinkedSpan(
	ctx context.Context,
	tracer trace.Tracer,
	name string,
	fn func(span trace.Span) error,
	opts ...trace.SpanStartOption,
) error {
	_, span, err := StartLinkedSpan(ctx, tracer, h, name, opts...)
	if err != nil {
		return err
	}
	defer span.End()
	if err := fn(span); err != nil {
		traceutil.SpanError(span, err)
		return err
	}
	traceutil.SpanOK(span)
	return nil
}
