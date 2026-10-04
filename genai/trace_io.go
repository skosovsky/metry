package genai

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// RecordTraceIO mirrors typed GenAI input/output payloads onto the span in ctx using OTLP GenAI semconv keys.
// Respects tracker payload recording and truncation settings; never logs arbitrary maps.
func (t *Tracker) RecordTraceIO(ctx context.Context, input Payload, output Payload) error {
	if !t.cfg.RecordPayloads() {
		return nil
	}
	merged := Payload{
		SystemInstructions: input.SystemInstructions,
		InputMessages:      input.InputMessages,
		OutputMessages:     output.OutputMessages,
	}
	attrs, err := buildPayloadAttributes(merged, t.cfg)
	if err != nil {
		return err
	}
	if len(attrs) > 0 {
		mutateSpan(ctx, func(span trace.Span) {
			span.SetAttributes(attrs...)
		})
	}
	return nil
}
