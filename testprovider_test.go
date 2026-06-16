package metry

import (
	"context"
	"testing"

	"github.com/skosovsky/metry/testutil"
)

func newTestProvider(t *testing.T, opts ...Option) (*Provider, *testutil.InMemoryTraceExporter) {
	t.Helper()
	mem := testutil.NewInMemoryTraceExporter()
	allOpts := append([]Option{WithServiceName("test"), WithExporter(mustSpanExporter(mem))}, opts...)
	provider, err := New(context.Background(), allOpts...)
	if err != nil {
		t.Fatalf("newTestProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider, mem
}

func testMetricExporter(mem *testutil.InMemoryMetricExporter) MetricExporter {
	return mustMetricExporter(mem)
}
