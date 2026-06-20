package metry

import (
	"context"

	"github.com/skosovsky/metry/propagation"
)

// InjectToMap writes W3C propagation fields into a protocol-level carrier using this provider's propagator.
// Nil provider is a no-op by design (safe for optional wiring).
func (p *Provider) InjectToMap(ctx context.Context, carrier map[string]any) {
	if p == nil {
		return
	}
	propagation.InjectToMap(ctx, p.textMapPropagator(), carrier)
}

// ExtractFromMap restores trace context from a protocol-level carrier using this provider's propagator.
// Nil provider returns ctx unchanged (safe for optional wiring).
func (p *Provider) ExtractFromMap(ctx context.Context, carrier map[string]any) context.Context {
	if p == nil {
		return ctx
	}
	return propagation.ExtractFromMap(ctx, p.textMapPropagator(), carrier)
}
