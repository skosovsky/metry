package metry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestInit_ValidOptions_Succeeds(t *testing.T) {
	ctx := context.Background()
	opts := Options{
		ServiceName:    "test-svc",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		TraceRatio:     new(1.0),
		// Nil exporters -> noop
	}

	shutdown, err := Init(ctx, opts)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	tracer := GlobalTracer()
	require.NotNil(t, tracer)
	meter := GlobalMeter()
	require.NotNil(t, meter)
}

func TestInit_MissingServiceName_ReturnsError(t *testing.T) {
	ctx := context.Background()
	opts := Options{
		ServiceName: "",
	}

	shutdown, err := Init(ctx, opts)
	require.ErrorIs(t, err, ErrServiceNameRequired)
	require.Nil(t, shutdown)
}

func TestInit_DefaultTraceRatio(t *testing.T) {
	ctx := context.Background()
	opts := Options{
		ServiceName: "test-svc",
		// TraceRatio: nil -> should default to 1.0
	}

	shutdown, err := Init(ctx, opts)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	// Just ensure we get a tracer (sampler would use 1.0)
	tr := GlobalTracer()
	require.NotNil(t, tr)
}

func TestInit_ZeroTraceRatio(t *testing.T) {
	ctx := context.Background()
	opts := Options{
		ServiceName: "test-svc",
		TraceRatio:  Float64(0), // 0% sampling (NeverSample)
	}

	shutdown, err := Init(ctx, opts)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	tr := GlobalTracer()
	require.NotNil(t, tr)
}

func TestShutdown_Idempotent(t *testing.T) {
	ctx := context.Background()
	opts := Options{ServiceName: "test-svc"}

	shutdown, err := Init(ctx, opts)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	err1 := shutdown(ctx)
	require.NoError(t, err1)
	err2 := shutdown(ctx)
	require.NoError(t, err2)
}

func TestInit_WithTraceExporter_Succeeds(t *testing.T) {
	ctx := context.Background()
	opts := Options{
		ServiceName: "test-svc",
		TraceExporter: &TraceExporter{
			create: func(_ context.Context, _ *resource.Resource) (sdktrace.SpanExporter, error) {
				return noopSpanExporter{}, nil
			},
		},
	}

	shutdown, err := Init(ctx, opts)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	tr := GlobalTracer()
	require.NotNil(t, tr)
}
