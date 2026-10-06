package metry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/skosovsky/metry"
)

// Embedding only the public interfaces deliberately hides SDK status getters.
type publicSpan struct{ trace.Span }
type publicTracer struct{ trace.Tracer }
type publicTracerProvider struct{ trace.TracerProvider }

func (p publicTracerProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return publicTracer{Tracer: p.TracerProvider.Tracer(name, opts...)}
}

func (t publicTracer) Start(
	ctx context.Context, name string, opts ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	//nolint:spancheck // Public wrapper transfers span lifetime to the caller.
	ctx, span := t.Tracer.Start(ctx, name, opts...)
	wrapped := publicSpan{Span: span}
	return trace.ContextWithSpan(ctx, wrapped), wrapped //nolint:spancheck // Caller owns wrapped span.
}

func TestProviderSpanEndPreservesOutcome(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		for _, status := range []codes.Code{codes.Unset, codes.Error, codes.Ok} {
			t.Run(status.String()+map[bool]string{false: "/sdk", true: "/public"}[wrapped], func(t *testing.T) {
				// Arrange: actual recording SDK, optionally hidden by public-only wrappers.
				exporter := tracetest.NewInMemoryExporter()
				sdk := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
				t.Cleanup(func() { require.NoError(t, sdk.Shutdown(context.Background())) })
				var tp trace.TracerProvider = sdk
				if wrapped {
					tp = publicTracerProvider{TracerProvider: tp}
				}
				provider, err := metry.FromSDK(metry.SDKDependencies{
					TracerProvider: tp, MeterProvider: noop.NewMeterProvider(), Propagator: propagation.TraceContext{},
				})
				require.NoError(t, err)
				ctx, end, err := provider.StartSpan(context.Background(), "status-test", "operation")
				require.NoError(t, err)
				if status != codes.Unset {
					trace.SpanFromContext(ctx).SetStatus(status, "failed")
				}

				// Act.
				end()
				end()

				// Assert: ending supplies no new evidence of success.
				spans := exporter.GetSpans()
				require.Len(t, spans, 1)
				require.Equal(t, status, spans[0].Status.Code)
				if status == codes.Error {
					require.Equal(t, "failed", spans[0].Status.Description)
				}
			})
		}
	}
}

func TestProviderSpanEndNoop(t *testing.T) {
	// Arrange.
	provider, err := metry.FromSDK(metry.SDKDependencies{
		TracerProvider: tracenoop.NewTracerProvider(),
		MeterProvider:  noop.NewMeterProvider(),
		Propagator:     propagation.TraceContext{},
	})
	require.NoError(t, err)
	ctx, end, err := provider.StartSpan(context.Background(), "status-test", "noop")
	require.NoError(t, err)

	// Act.
	trace.SpanFromContext(ctx).SetStatus(codes.Error, "failed")
	end()
	end()

	// Assert.
	require.False(t, trace.SpanFromContext(ctx).IsRecording())
	require.False(t, trace.SpanContextFromContext(ctx).IsValid())
}
