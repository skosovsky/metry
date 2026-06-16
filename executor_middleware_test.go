package metry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestExecutorWrap_CreatesSpan(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewInMemoryTraceExporter()
	metricMem := testutil.NewInMemoryMetricExporter()
	provider, err := metry.New(
		ctx,
		metry.WithServiceName("root-exec"),
		metry.WithExporter(metrytest.MetrySpanExporter(mem)),
		metry.WithMetricExporter(metrytest.MetryMetricExporter(metricMem)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	wrapped := metry.ExecutorWrap(provider, "root-op", func(_ context.Context, n int) (int, error) {
		return n * 2, nil
	})

	out, err := wrapped(ctx, 21)
	require.NoError(t, err)
	assert.Equal(t, 42, out)

	require.NoError(t, provider.ForceFlush(ctx))
	assert.GreaterOrEqual(t, mem.Len(), 1)
}
