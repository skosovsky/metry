package propagation

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
)

// InjectToMap replaces only fields declared by the propagator, including custom fields.
// It clears stale declared fields, preserves all host keys, and ignores undeclared writes.
// A nil carrier or nil (including typed nil) propagator is a no-op.
func InjectToMap(ctx context.Context, propagator propagation.TextMapPropagator, carrier map[string]any) {
	if carrier == nil || nilInterface(propagator) {
		return
	}
	for _, field := range propagator.Fields() {
		delete(carrier, field)
	}
	stringCarrier := make(stringMapCarrier)
	propagator.Inject(ctx, stringCarrier)
	for _, k := range propagator.Fields() {
		v := stringCarrier[k]
		if v == "" {
			continue
		}
		carrier[k] = v
	}
}

// ExtractFromMap supplies nonempty string values from declared propagator fields only.
// The propagator defines trace and baggage acceptance (use Wrap to apply Policy).
// Nil propagators, nil/empty carriers, and carriers without usable fields return ctx.
func ExtractFromMap(
	ctx context.Context,
	propagator propagation.TextMapPropagator,
	carrier map[string]any,
) context.Context {
	if nilInterface(propagator) || len(carrier) == 0 {
		return ctx
	}
	stringCarrier := make(stringMapCarrier)
	for _, k := range propagator.Fields() {
		v := carrier[k]
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
