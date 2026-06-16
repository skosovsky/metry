package metry

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"
)

// SpanEnd ends a span started with Provider.StartSpan.
type SpanEnd func()

// StartSpanOption configures span start without exposing OpenTelemetry types.
type StartSpanOption func(*spanStartConfig)

type spanStartConfig struct {
	otelOpts []trace.SpanStartOption
}

// WithSpanAttributes adds typed attributes at span start.
func WithSpanAttributes(attrs ...Attribute) StartSpanOption {
	return func(c *spanStartConfig) {
		kv := attributesToOTel(attrs)
		if len(kv) > 0 {
			c.otelOpts = append(c.otelOpts, trace.WithAttributes(kv...))
		}
	}
}

// StartSpan creates a span using the provider tracer without exposing OTel types to callers.
func (p *Provider) StartSpan(
	ctx context.Context,
	tracerName, spanName string,
	opts ...StartSpanOption,
) (context.Context, SpanEnd, error) {
	if p == nil || p.otelTracer == nil {
		return ctx, func() {}, ErrNilTracerProvider
	}
	var cfg spanStartConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	//nolint:spancheck // caller ends span via SpanEnd
	ctx, span := p.otelTracer.Tracer(tracerName).Start(ctx, spanName, cfg.otelOpts...)
	end := func() { traceutil.EndSpanOKIfUnset(span) }
	return ctx, end, nil //nolint:spancheck // caller ends span via SpanEnd
}
