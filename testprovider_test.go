package metry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/skosovsky/metry/testutil"
)

func newTestProvider(t *testing.T, opts ...Option) (*Provider, *testutil.InMemoryTraceExporter) {
	t.Helper()
	mem := testutil.NewInMemoryTraceExporter()
	allOpts := append([]Option{WithServiceName("test"), WithExporter(mem.SDKSpanExporter())}, opts...)
	provider, err := New(context.Background(), allOpts...)
	if err != nil {
		t.Fatalf("newTestProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider, mem
}

func testMetricExporter(mem *testutil.InMemoryMetricExporter) sdkmetric.Exporter {
	return mem.SDKExporter()
}
