package otlp_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/skosovsky/metry"
)

// This consumer deliberately imports neither metrytest nor metry/internal.
func TestPublicAPI_ExportsTraceAndMetricOverOTLPHTTP(t *testing.T) {
	// Arrange: a real loopback OTLP protobuf receiver.
	var mu sync.Mutex
	var traceNames, metricNames []string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/traces":
			names, decodeErr := decodeTraceNames(raw)
			if decodeErr != nil {
				t.Error(decodeErr)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			traceNames = append(traceNames, names...)
		case "/v1/metrics":
			names, decodeErr := decodeMetricNames(raw)
			if decodeErr != nil {
				t.Error(decodeErr)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			metricNames = append(metricNames, names...)
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(receiver.Close)
	ctx := context.Background()
	endpoint := strings.TrimPrefix(receiver.URL, "http://")
	traces, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithInsecure(),
		otlptracehttp.WithCompression(otlptracehttp.NoCompression), otlptracehttp.WithURLPath("/v1/traces"))
	require.NoError(t, err)
	metrics, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpoint(endpoint), otlpmetrichttp.WithInsecure(),
		otlpmetrichttp.WithCompression(otlpmetrichttp.NoCompression), otlpmetrichttp.WithURLPath("/v1/metrics"))
	if err != nil {
		_ = traces.Shutdown(ctx)
	}
	require.NoError(t, err)
	provider, err := metry.New(
		ctx,
		metry.WithServiceName("external-consumer"),
		metry.WithExporter(traces),
		metry.WithMetricExporter(metrics),
	)
	if err != nil {
		_ = traces.Shutdown(ctx)
		_ = metrics.Shutdown(ctx)
	}
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(ctx)) })
	registry := metry.NewMetricsRegistry(provider)
	calls, err := registry.NewCounter("integration_calls")
	require.NoError(t, err)
	// Act.
	spanCtx, end, err := provider.StartSpan(ctx, "integration", "actual-call")
	require.NoError(t, err)
	calls.Add(spanCtx, 1, metry.Labels{"operation": "actual-call"})
	end()
	require.NoError(t, provider.ForceFlush(ctx))
	// Assert on receiver-decoded exports, not recording inputs.
	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, traceNames, "actual-call")
	require.Contains(t, metricNames, "integration_calls")
}

func decodeTraceNames(raw []byte) ([]string, error) {
	req := &collectortrace.ExportTraceServiceRequest{}
	if err := proto.Unmarshal(raw, req); err != nil {
		return nil, err
	}
	var names []string
	for _, resource := range req.GetResourceSpans() {
		for _, scope := range resource.GetScopeSpans() {
			for _, span := range scope.GetSpans() {
				names = append(names, span.GetName())
			}
		}
	}
	return names, nil
}

func decodeMetricNames(raw []byte) ([]string, error) {
	req := &collectormetric.ExportMetricsServiceRequest{}
	if err := proto.Unmarshal(raw, req); err != nil {
		return nil, err
	}
	var names []string
	for _, resource := range req.GetResourceMetrics() {
		for _, scope := range resource.GetScopeMetrics() {
			for _, instrument := range scope.GetMetrics() {
				names = append(names, instrument.GetName())
			}
		}
	}
	return names, nil
}
