// Package metrytest provides test helpers that depend on metry (avoids import cycles in package metry tests).
package metrytest

import (
	"context"
	"testing"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/testutil"
)

// NewTestProvider creates a metry Provider with an in-memory trace exporter for assertions.
func NewTestProvider(t *testing.T, opts ...metry.Option) (*metry.Provider, *testutil.InMemoryTraceExporter) {
	t.Helper()
	mem := testutil.NewInMemoryTraceExporter()
	allOpts := append([]metry.Option{
		metry.WithServiceName("test"),
		metry.WithExporter(MetrySpanExporter(mem)),
	}, opts...)
	provider, err := metry.New(context.Background(), allOpts...)
	if err != nil {
		t.Fatalf("metrytest.NewTestProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider, mem
}
