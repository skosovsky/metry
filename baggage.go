package metry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/baggage"
)

// SetBaggageValue injects a key-value pair into the context's baggage.
// Keys and values must comply with W3C Baggage; invalid key/value returns a wrapped error.
func SetBaggageValue(ctx context.Context, key, value string) (context.Context, error) {
	member, err := baggage.NewMember(key, value)
	if err != nil {
		return ctx, fmt.Errorf(
			"metry: invalid baggage key/value (W3C standard prohibits special chars/spaces in keys): %w",
			err,
		)
	}
	b := baggage.FromContext(ctx)
	b, err = b.SetMember(member)
	if err != nil {
		return ctx, fmt.Errorf("metry: set baggage member: %w", err)
	}
	return baggage.ContextWithBaggage(ctx, b), nil
}

// BaggageValue retrieves a value for the given key from the context's baggage.
// Returns an empty string if the key does not exist.
func BaggageValue(ctx context.Context, key string) string {
	b := baggage.FromContext(ctx)
	return b.Member(key).Value()
}
