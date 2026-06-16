package genai

import (
	"context"
	"errors"
	"fmt"

	"github.com/skosovsky/metry"
)

// ErrInvalidAsyncHandle is returned when async APIs receive a zero or invalid handle.
var ErrInvalidAsyncHandle = metry.ErrInvalidAsyncHandle

// ErrAdapterRequired is returned when RecordModelInteraction is called with a nil adapter.
var ErrAdapterRequired = errors.New("genai: adapter is required")

// ProviderAdapter transforms vendor-specific request/response types into metry's canonical GenAI types.
// Raw parameters are intentionally any: adapters perform type assertions at the boundary.
type ProviderAdapter interface {
	ParseRequest(req any) (Payload, Meta, error)
	ParseResponse(resp any) (Payload, Usage, error)
}

// RecordModelInteraction parses raw request/response via adapter and records a standard interaction.
func (t *Tracker) RecordModelInteraction(
	ctx context.Context,
	adapter ProviderAdapter,
	rawReq any,
	rawResp any,
) error {
	if adapter == nil {
		return ErrAdapterRequired
	}

	payload, meta, err := adapter.ParseRequest(rawReq)
	if err != nil {
		return fmt.Errorf("genai: parse request: %w", err)
	}

	outPayload, usage, err := adapter.ParseResponse(rawResp)
	if err != nil {
		return fmt.Errorf("genai: parse response: %w", err)
	}

	merged := mergePayload(payload, outPayload)
	return t.RecordInteraction(ctx, meta, merged, usage)
}

func mergePayload(reqPayload, respPayload Payload) Payload {
	return Payload{
		SystemInstructions: reqPayload.SystemInstructions,
		InputMessages:      reqPayload.InputMessages,
		OutputMessages:     respPayload.OutputMessages,
	}
}
