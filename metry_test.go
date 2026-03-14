package metry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestInit_ValidOptions_Succeeds(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, WithServiceName("test-svc"), WithServiceVersion("1.0.0"), WithEnvironment("test"))
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
	shutdown, err := Init(ctx, WithEnvironment("test"))
	require.ErrorIs(t, err, ErrServiceNameRequired)
	require.Nil(t, shutdown)
}

func TestInit_DefaultTraceRatio(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, WithServiceName("test-svc"))
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	tr := GlobalTracer()
	require.NotNil(t, tr)
}

func TestInit_ZeroTraceRatio(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, WithServiceName("test-svc"), WithTraceRatio(0))
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
	shutdown, err := Init(ctx, WithServiceName("test-svc"))
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	err1 := shutdown(ctx)
	require.NoError(t, err1)
	err2 := shutdown(ctx)
	require.NoError(t, err2)
}

func TestInit_WithTraceExporter_Succeeds(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, WithServiceName("test-svc"), WithTraceExporter(&TraceExporter{
		create: func(_ context.Context, _ *resource.Resource) (sdktrace.SpanExporter, error) {
			return noopSpanExporter{}, nil
		},
	}))
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	tr := GlobalTracer()
	require.NotNil(t, tr)
}

func TestInit_MetricExporterFailure_RollbackTraceProvider(t *testing.T) {
	ctx := context.Background()
	primaryErr := errors.New("metric exporter failed")
	shutdown, err := Init(ctx,
		WithServiceName("test-svc"),
		WithTraceExporter(&TraceExporter{
			create: func(_ context.Context, _ *resource.Resource) (sdktrace.SpanExporter, error) {
				return noopSpanExporter{}, nil
			},
		}),
		WithMetricExporter(&MetricExporter{
			create: func(_ context.Context, _ *resource.Resource) (sdkmetric.Exporter, error) {
				return nil, primaryErr
			},
		}),
	)
	require.Error(t, err)
	require.Nil(t, shutdown)
	require.True(t, errors.Is(err, primaryErr) || errors.Unwrap(err) == primaryErr, "error should wrap primary failure")
}
