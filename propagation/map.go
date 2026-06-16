package propagation

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
)

// W3C propagation field names extracted from map carriers.
const (
	w3cTraceParent = "traceparent"
	w3cTraceState  = "tracestate"
	w3cBaggage     = "baggage"
)

func isW3CPropagationKey(key string) bool {
	switch key {
	case w3cTraceParent, w3cTraceState, w3cBaggage:
		return true
	default:
		return false
	}
}

// InjectToMap writes W3C propagation fields into carrier. Existing non-telemetry keys are preserved.
func InjectToMap(ctx context.Context, propagator propagation.TextMapPropagator, carrier map[string]any) {
	if carrier == nil || propagator == nil {
		return
	}
	stringCarrier := make(stringMapCarrier)
	propagator.Inject(ctx, stringCarrier)
	for k, v := range stringCarrier {
		carrier[k] = v
	}
}

// ExtractFromMap restores trace context from carrier W3C fields only.
func ExtractFromMap(
	ctx context.Context,
	propagator propagation.TextMapPropagator,
	carrier map[string]any,
) context.Context {
	if propagator == nil || len(carrier) == 0 {
		return ctx
	}
	stringCarrier := make(stringMapCarrier)
	for k, v := range carrier {
		if !isW3CPropagationKey(k) {
			continue
		}
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		stringCarrier.Set(k, s)
	}
	if len(stringCarrier) == 0 {
		return ctx
	}
	return propagator.Extract(ctx, stringCarrier)
}
