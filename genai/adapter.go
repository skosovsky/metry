package genai

import (
	"context"
	"errors"

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
	ParseResponse(resp any) (ResponseObservation, error)
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
		return invalidObservation("adapter_request", "parse_failed")
	}

	response, err := adapter.ParseResponse(rawResp)
	if err != nil {
		return invalidObservation("adapter_response", "parse_failed")
	}

	meta.ResponseModel = response.Metadata.Model
	meta.Status = response.Status
	meta.ErrorType = response.ErrorType
	merged := mergePayload(payload, response.Payload)
	return t.RecordInteraction(ctx, meta, merged, response.Usage)
}

func mergePayload(reqPayload, respPayload Payload) Payload {
	return Payload{
		SystemInstructions: reqPayload.SystemInstructions,
		InputMessages:      reqPayload.InputMessages,
		OutputMessages:     respPayload.OutputMessages,
	}
}

// ResponseMetadata contains only facts observed in a received response.
type ResponseMetadata struct{ Model string }

// ResponseObservation is a stateless adapter result; no request fallback applies.
type ResponseObservation struct {
	Payload   Payload
	Usage     Usage
	Metadata  ResponseMetadata
	Status    Outcome
	ErrorType string
}
