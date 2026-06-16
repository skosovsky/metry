package metrytest

import (
	"context"

	"github.com/skosovsky/metry"
)

// BaggageMember returns the string value of a baggage member key from ctx.
func BaggageMember(ctx context.Context, key string) string {
	return metry.BaggageMember(ctx, key)
}
