package propagation

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type stringMapCarrier map[string]string

func (c stringMapCarrier) Get(key string) string {
	if c == nil {
		return ""
	}
	return c[key]
}

func (c stringMapCarrier) Set(key, value string) {
	if c == nil {
		return
	}
	c[key] = value
}

func (c stringMapCarrier) Keys() []string {
	if len(c) == 0 {
		return nil
	}
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// InjectToJSON serializes the trace context from ctx using the global TextMapPropagator.
func InjectToJSON(ctx context.Context) ([]byte, error) {
	return InjectToJSONWithPropagator(ctx, otel.GetTextMapPropagator())
}

// ExtractFromJSON restores trace context from JSON bytes using the global TextMapPropagator.
// Invalid or empty payload returns ctx unchanged.
func ExtractFromJSON(ctx context.Context, payload []byte) context.Context {
	return ExtractFromJSONWithPropagator(ctx, otel.GetTextMapPropagator(), payload)
}

// InjectToJSONWithPropagator serializes ctx with an explicit propagator (tests, custom Provider.Propagator).
func InjectToJSONWithPropagator(ctx context.Context, propagator propagation.TextMapPropagator) ([]byte, error) {
	if propagator == nil {
		propagator = otel.GetTextMapPropagator()
	}
	carrier := make(stringMapCarrier)
	propagator.Inject(ctx, carrier)
	if len(carrier) == 0 {
		return json.Marshal(stringMapCarrier{})
	}
	return json.Marshal(carrier)
}

// ExtractFromJSONWithPropagator restores ctx from JSON using an explicit propagator.
func ExtractFromJSONWithPropagator(
	ctx context.Context,
	propagator propagation.TextMapPropagator,
	payload []byte,
) context.Context {
	if len(payload) == 0 {
		return ctx
	}
	if propagator == nil {
		propagator = otel.GetTextMapPropagator()
	}
	var carrier stringMapCarrier
	if err := json.Unmarshal(payload, &carrier); err != nil {
		return ctx
	}
	if len(carrier) == 0 {
		return ctx
	}
	return propagator.Extract(ctx, carrier)
}
