package genai

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/traceutil"
)

// ErrInvalidSpanContext is returned when async APIs receive a zero or invalid SpanContext.
var ErrInvalidSpanContext = errors.New("genai: invalid span context")

// ProviderAdapter transforms vendor-specific request/response types into metry's canonical GenAI types.
// Raw parameters are intentionally any: adapters perform type assertions at the boundary.
type ProviderAdapter interface {
	ParseRequest(req any) (Payload, Meta, error)
	ParseResponse(resp any) (Payload, Usage, error)
}

// RecordModelInteraction parses raw request/response via adapter and records a standard interaction.
func (t *Tracker) RecordModelInteraction(
	ctx context.Context,
	span trace.Span,
	adapter ProviderAdapter,
	rawReq any,
	rawResp any,
) error {
	if adapter == nil {
		return errors.New("genai: adapter is required")
	}

	payload, meta, err := adapter.ParseRequest(rawReq)
	if err != nil {
		traceutil.SpanError(span, err)
		return fmt.Errorf("genai: parse request: %w", err)
	}

	outPayload, usage, err := adapter.ParseResponse(rawResp)
	if err != nil {
		traceutil.SpanError(span, err)
		return fmt.Errorf("genai: parse response: %w", err)
	}

	merged := mergePayload(payload, outPayload)
	t.RecordInteraction(ctx, span, meta, merged, usage)
	return nil
}

func mergePayload(reqPayload, respPayload Payload) Payload {
	return Payload{
		SystemInstructions: reqPayload.SystemInstructions,
		InputMessages:      reqPayload.InputMessages,
		OutputMessages:     respPayload.OutputMessages,
	}
}
