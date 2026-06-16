// Package genaiwire connects genai to provider internals without exposing OTel types publicly.
package genaiwire

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// MeterTracer resolves GenAI meter and tracer from a provider.
var MeterTracer func(p any) (metric.Meter, trace.Tracer, error)

// NewHintSampler wraps a base sampler with GenAI keep-hint support.
// The argument and result are metry.TraceSampler values; any avoids an import cycle with metry.
var NewHintSampler func(base any) any
