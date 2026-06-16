package metrytest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
)

// StartSpan starts a span via provider.StartSpan and returns ctx, end, and error.
func StartSpan(
	t *testing.T,
	provider *metry.Provider,
	tracerName, spanName string,
	opts ...metry.StartSpanOption,
) (context.Context, func(), error) {
	t.Helper()
	return provider.StartSpan(context.Background(), tracerName, spanName, opts...)
}

// AsyncHandleFromSpan starts a span, captures an async handle, ends the span, and returns the handle.
func AsyncHandleFromSpan(
	t *testing.T,
	provider *metry.Provider,
	tracerName, spanName string,
) metry.AsyncHandle {
	t.Helper()
	ctx, end, err := provider.StartSpan(context.Background(), tracerName, spanName)
	require.NoError(t, err)
	handle, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	end()
	return handle
}
