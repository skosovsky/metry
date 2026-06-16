package metry

import (
	"context"

	"go.opentelemetry.io/otel/baggage"
)

// BaggageMember returns the string value of a baggage member key from ctx.
func BaggageMember(ctx context.Context, key string) string {
	return baggage.FromContext(ctx).Member(key).Value()
}
