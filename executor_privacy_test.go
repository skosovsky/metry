package metry_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/diagnostic"
)

type privacyPanic struct{ formats *int }

func (p *privacyPanic) String() string { *p.formats++; return "SENTINEL_SECRET" }

func TestExecutorDiagnosticFanout(t *testing.T) {
	for _, mode := range []string{"default", "sanitized", "raw", "failure", "panic_callback", "small", "failure_small"} {
		t.Run(mode, func(t *testing.T) {
			config := executorDiagnosticConfig(mode)
			policy, err := diagnostic.NewPolicy(config)
			require.NoError(t, err)
			sinks := []*tracetest.InMemoryExporter{tracetest.NewInMemoryExporter(), tracetest.NewInMemoryExporter()}
			tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(sinks[0]), sdktrace.WithSyncer(sinks[1]))
			provider, err := metry.FromSDK(
				metry.SDKDependencies{
					TracerProvider: tp,
					MeterProvider:  noop.NewMeterProvider(),
					Propagator:     propagation.TraceContext{},
				},
				metry.WithSDKDiagnosticPolicy(policy),
			)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tp.Shutdown(context.Background())) })
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			business := errors.New("SENTINEL_SECRET")
			fn := metry.ExecutorWrap(
				provider,
				"op",
				func(context.Context, int) (int, error) { return 42, business },
				metry.WithExecutorLogger(logger),
			)
			result, err := fn(context.Background(), 0)
			require.Equal(t, 42, result)
			require.Same(t, business, err)
			formats := 0
			panicValue := &privacyPanic{&formats}
			panicFn := metry.ExecutorWrap(
				provider,
				"panic",
				func(context.Context, int) (int, error) { panic(panicValue) },
				metry.WithExecutorLogger(logger),
			)
			func() {
				defer func() { require.Same(t, panicValue, recover()) }()
				_, _ = panicFn(context.Background(), 0)
			}()
			if mode == "default" {
				require.Zero(t, formats)
			}
			checkExecutorSinks(t, mode, sinks)
			if mode == "raw" {
				require.Contains(t, logs.String(), "SENTINEL_SECRET")
			} else {
				require.NotContains(t, logs.String(), "SENTINEL_SECRET")
			}
		})
	}
}

func checkExecutorSinks(t *testing.T, mode string, sinks []*tracetest.InMemoryExporter) {
	t.Helper()
	for _, sink := range sinks {
		spans := sink.GetSpans()
		require.Len(t, spans, 2)
		for _, span := range spans {
			evidence := fmt.Sprint(span.Status, span.Events)
			if mode == "raw" {
				require.Contains(t, evidence, "SENTINEL_SECRET")
			} else {
				require.NotContains(t, evidence, "SENTINEL_SECRET")
			}
			require.Len(t, span.Events, 1)
			checkExecutorSmallBudget(t, mode, span)
		}
	}
}

func executorDiagnosticConfig(mode string) diagnostic.Config {
	config := diagnostic.Config{}
	switch mode {
	case "small":
		config.MaxOutputBytes = 1
	case "failure_small":
		config.MaxOutputBytes = 1
		config.Sanitizer = func(diagnostic.Channel, string) (string, error) { return "", errors.New("SENTINEL_SECRET") }
	case "sanitized":
		config.Sanitizer = func(diagnostic.Channel, string) (string, error) { return "redacted", nil }
	case "raw":
		config.Raw = true
	case "failure":
		config.Sanitizer = func(diagnostic.Channel, string) (string, error) { return "", errors.New("SENTINEL_SECRET") }
	case "panic_callback":
		config.Sanitizer = func(diagnostic.Channel, string) (string, error) { panic("SENTINEL_SECRET") }
	}
	return config
}

func TestLinkedDiagnosticPanicEndsBeforeReraise(t *testing.T) {
	ctx := context.Background()
	sink := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(sink))
	provider, err := metry.FromSDK(
		metry.SDKDependencies{
			TracerProvider: tp,
			MeterProvider:  noop.NewMeterProvider(),
			Propagator:     propagation.TraceContext{},
		},
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tp.Shutdown(ctx)) })
	formats := 0
	original := &privacyPanic{&formats}
	func() {
		defer func() { require.Same(t, original, recover()) }()
		_ = metry.NoopAsyncHandle().
			RecordLinkedSpan(ctx, provider, "linked", func(metry.LinkedSpanWriter) error { panic(original) })
	}()
	require.Zero(t, formats)
	spans := sink.GetSpans()
	require.Len(t, spans, 1)
	require.NotContains(t, fmt.Sprint(spans[0].Status, spans[0].Events), "SENTINEL_SECRET")
	require.Len(t, spans[0].Events, 1)
	business := errors.New("SENTINEL_SECRET")
	err = metry.NoopAsyncHandle().
		RecordLinkedSpan(ctx, provider, "linked-error", func(metry.LinkedSpanWriter) error { return business })
	require.Same(t, business, err)
	spans = sink.GetSpans()
	require.Len(t, spans, 2)
	require.NotContains(t, fmt.Sprint(spans[1].Status, spans[1].Events), "SENTINEL_SECRET")
}

func checkExecutorSmallBudget(t *testing.T, mode string, span tracetest.SpanStub) {
	t.Helper()
	if mode == "small" || mode == "failure_small" {
		require.Empty(t, span.Status.Description)
		for _, attr := range span.Events[0].Attributes {
			if attr.Key == "exception.message" || attr.Key == "exception.stacktrace" {
				require.LessOrEqual(t, len(attr.Value.AsString()), 1)
			}
		}
	}
}
