package metrytest

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/metrytestwire"
)

// NewProviderWithDeps builds a Provider for tests in metry and subpackages.
// Prefer metry.New or metrytest.NewTestProvider for integration tests.
func NewProviderWithDeps(tracer trace.TracerProvider, meter metric.MeterProvider) *metry.Provider {
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	return mustAs[*metry.Provider](metrytestwire.NewProviderFromDeps(tracer, meter, prop), "NewProviderFromDeps")
}
