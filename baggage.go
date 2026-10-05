package metry

import (
	"context"

	"go.opentelemetry.io/otel/baggage"
)

// BaggageMember reads untrusted raw transport baggage for host inspection.
// Presence and typed markers do not establish provenance for logs or GenAI scope.
func BaggageMember(ctx context.Context, key string) string {
	return baggage.FromContext(ctx).Member(key).Value()
}
