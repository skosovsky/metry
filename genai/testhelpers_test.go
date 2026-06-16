package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func newTestTracker(t *testing.T, opts ...Option) (*Tracker, *metry.Provider, *testutil.InMemoryTraceExporter) {
	t.Helper()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("genai-test"))
	tracker, err := NewTrackerFromProvider(provider, opts...)
	require.NoError(t, err)
	return tracker, provider, mem
}

func newTestTrackerWithMetrics(
	t *testing.T,
	opts ...Option,
) (*Tracker, *metry.Provider, *testutil.InMemoryMetricExporter, *testutil.InMemoryTraceExporter) {
	t.Helper()
	memMetric := testutil.NewInMemoryMetricExporter()
	provider, memTrace := metrytest.NewTestProvider(t,
		metry.WithServiceName("genai-test"),
		metry.WithMetricExporter(metrytest.MetryMetricExporter(memMetric)),
	)
	tracker, err := NewTrackerFromProvider(provider, opts...)
	require.NoError(t, err)
	return tracker, provider, memMetric, memTrace
}

func newTestTrackerWithSampler(
	t *testing.T,
	sampler metry.TraceSampler,
) (*Tracker, *metry.Provider, *testutil.InMemoryTraceExporter) {
	t.Helper()
	provider, mem := metrytest.NewTestProvider(t,
		metry.WithServiceName("genai-test"),
		metry.WithSampler(sampler),
	)
	tracker, err := NewTrackerFromProvider(provider)
	require.NoError(t, err)
	return tracker, provider, mem
}

func flushTestProvider(t *testing.T, provider *metry.Provider) {
	t.Helper()
	require.NoError(t, provider.ForceFlush(context.Background()))
}
