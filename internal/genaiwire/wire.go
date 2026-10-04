// Package genaiwire connects genai to provider internals without exposing OTel types publicly.
package genaiwire

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// MeterTracer resolves GenAI meter and tracer from a provider.
var MeterTracer func(p any) (metric.Meter, trace.Tracer, error)

// AsyncSpanContext resolves an opaque async token for internal causal linking.
var AsyncSpanContext func(handle any) (trace.SpanContext, error)
