package metry_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/diagnostic"
)

func TestHTTPPanicPrivacyBeforeTwoExporters(t *testing.T) {
	for _, mode := range []string{"default", "sanitized", "raw", "failure", "panic_callback", "small", "failure_small"} {
		for _, nested := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/executor=%t", mode, nested), func(t *testing.T) {
				checkHTTPPanicPrivacy(t, mode, nested)
			})
		}
	}
}

func checkHTTPPanicPrivacy(t *testing.T, mode string, nested bool) {
	t.Helper()
	policy, err := diagnostic.NewPolicy(executorDiagnosticConfig(mode))
	require.NoError(t, err)
	sinks := []*tracetest.InMemoryExporter{tracetest.NewInMemoryExporter(), tracetest.NewInMemoryExporter()}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(sinks[0]), sdktrace.WithSyncer(sinks[1]))
	t.Cleanup(func() { require.NoError(t, tp.Shutdown(context.Background())) })
	provider, err := metry.FromSDK(
		metry.SDKDependencies{
			TracerProvider: tp,
			MeterProvider:  noop.NewMeterProvider(),
			Propagator:     propagation.TraceContext{},
		},
		metry.WithSDKDiagnosticPolicy(policy),
	)
	require.NoError(t, err)
	formats := 0
	original := &privacyPanic{&formats}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(original) })
	if nested {
		executor := metry.ExecutorWrap(
			provider,
			"executor",
			func(context.Context, int) (int, error) { panic(original) },
		)
		next = func(_ http.ResponseWriter, r *http.Request) { _, _ = executor(r.Context(), 0) }
	}
	handler := metry.HTTPHandler(provider, next, "http")
	func() {
		defer func() { require.Same(t, original, recover()) }()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.test/", nil))
	}()
	if mode == "default" || mode == "small" {
		require.Zero(t, formats)
	}
	want := 1
	if nested {
		want = 2
	}
	for _, sink := range sinks {
		spans := sink.GetSpans()
		require.Len(t, spans, want)
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
