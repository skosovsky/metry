package genai

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// RecordTraceIO mirrors typed GenAI input/output payloads onto span attributes using OTLP GenAI semconv keys.
// Respects tracker payload recording and truncation settings; never logs arbitrary maps.
func (t *Tracker) RecordTraceIO(_ context.Context, span trace.Span, input Payload, output Payload) {
	if !t.cfg.RecordPayloads() {
		return
	}
	merged := Payload{
		SystemInstructions: input.SystemInstructions,
		InputMessages:      input.InputMessages,
		OutputMessages:     output.OutputMessages,
	}
	attrs := buildPayloadAttributes(merged, t.cfg)
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
}
