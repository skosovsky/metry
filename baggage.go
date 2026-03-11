package metry

import (
	"context"

	"go.opentelemetry.io/otel/baggage"
)

// ContextWithBaggage injects a key-value pair into the context's baggage.
// This data will automatically propagate across HTTP and gRPC boundaries.
func ContextWithBaggage(ctx context.Context, key, value string) (context.Context, error) {
	member, err := baggage.NewMember(key, value)
	if err != nil {
		return ctx, err
	}
	b := baggage.FromContext(ctx)
	b, err = b.SetMember(member)
	if err != nil {
		return ctx, err
	}
	return baggage.ContextWithBaggage(ctx, b), nil
}

// BaggageValue retrieves a value for the given key from the context's baggage.
// Returns an empty string if the key does not exist.
func BaggageValue(ctx context.Context, key string) string {
	b := baggage.FromContext(ctx)
	return b.Member(key).Value()
}
