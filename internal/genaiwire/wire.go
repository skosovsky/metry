// Package genaiwire connects genai to provider internals without exposing OTel types publicly.
package genaiwire

import (
	"go.opentelemetry.io/otel/trace"
)

// AsyncSpanContext resolves an opaque async token for internal causal linking.
var AsyncSpanContext func(handle any) (trace.SpanContext, error)
