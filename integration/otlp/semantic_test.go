//go:build integration

package otlp_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
)

const (
	semanticSentinel = "private-wire-sentinel-never-export"
	semanticScope    = "metry/genai"
	semanticVersion  = "semconv-genai-e07f4eb+metry-v1"
	semanticSchema   = "https://opentelemetry.io/schemas/gen-ai-dev/1.42.0-dev"
	resourceSchema   = "https://opentelemetry.io/schemas/1.43.0"
	fixtureDuration  = 100 * time.Millisecond
	fixtureCost      = 0.125
)

type semanticReceiver struct {
	mu              sync.Mutex
	traces, metrics [][]byte
}

func (s *semanticReceiver) serve(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.URL.Path {
	case "/v1/traces":
		s.traces = append(s.traces, raw)
	case "/v1/metrics":
		s.metrics = append(s.metrics, raw)
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
}

func TestIntegrationPublicAPI_OTLPGenAISemantics(t *testing.T) {
	// Arrange: external public consumer, real exporters and loopback protobuf wire.
	// Explicit flush supplies the single cumulative snapshot under assertion.
	// External exporter settings must not schedule extra snapshots or change it.
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "3600000")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE", "cumulative")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION", "explicit_bucket_histogram")
	received := &semanticReceiver{}
	server := httptest.NewServer(http.HandlerFunc(received.serve))
	t.Cleanup(server.Close)
	ctx := t.Context()
	endpoint := strings.TrimPrefix(server.URL, "http://")
	traces, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithInsecure(),
		otlptracehttp.WithCompression(otlptracehttp.NoCompression), otlptracehttp.WithURLPath("/v1/traces"))
	require.NoError(t, err)
	metrics, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpoint(endpoint), otlpmetrichttp.WithInsecure(),
		otlpmetrichttp.WithCompression(otlpmetrichttp.NoCompression), otlpmetrichttp.WithURLPath("/v1/metrics"))
	if err != nil {
		_ = traces.Shutdown(context.Background())
	}
	require.NoError(t, err)
	provider, err := metry.New(
		ctx,
		metry.WithServiceName("semantic-consumer"),
		metry.WithServiceVersion("fixture-v1"),
		metry.WithExporter(traces),
		metry.WithMetricExporter(metrics),
	)
	if err != nil {
		_ = traces.Shutdown(context.Background())
		_ = metrics.Shutdown(context.Background())
	}
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	tracker, err := genai.NewTrackerFromProvider(provider, genai.WithPayloadPolicy(genai.RedactPayloadPolicy()))
	require.NoError(t, err)
	require.Equal(t, "metry.genai.semconv.e07f4eb.v1", tracker.Contract().Schema().ID())
	parentCtx, endParent, err := provider.StartSpan(ctx, "semantic-host", "origin")
	require.NoError(t, err)
	origin, err := metry.NewAsyncHandle(parentCtx)
	require.NoError(t, err)
	payload := genai.Payload{
		InputMessages: []genai.Message{
			{Role: "user", Parts: []genai.ContentPart{{Type: "text", Content: semanticSentinel}}},
		},
	}
	// Act: one known-zero completed child and one failed detached interval with
	// unknown final usage. Both use controlled privacy routes, never test exporters.
	require.NoError(
		t,
		tracker.Recorder().
			RecordOperation(parentCtx, genai.Operation{Name: "chat", Model: semanticSentinel}, genai.OperationResult{
				Status: genai.OperationStatusOK, Duration: fixtureDuration, Payload: payload,
				Usage: genai.Usage{
					InputTokens:  genai.Observe(0, genai.UnitTokens, genai.SourceMeasured),
					OutputTokens: genai.Observe(4, genai.UnitTokens, genai.SourceProvider),
					Cost:         genai.Observe(fixtureCost, genai.UnitCurrency, genai.SourceProvider),
					Currency:     "USD",
				},
			}),
	)
	_, failed, err := tracker.Recorder().
		StartOperation(parentCtx, genai.Operation{Name: "chat", Locality: genai.ExecutionRemote, Model: semanticSentinel}, genai.RecordingOptions{Detached: true, Links: []metry.AsyncHandle{origin}})
	require.NoError(t, err)
	require.NoError(
		t,
		failed.Finish(
			genai.OperationResult{
				Status:    genai.OperationStatusError,
				ErrorType: string(genai.ErrorClassCancelled),
				Payload:   payload,
			},
		),
	)
	endParent()
	require.NoError(t, provider.ForceFlush(context.Background()))
	// Assert on serialized receiver bytes and decoded exports, never input DTOs.
	received.mu.Lock()
	defer received.mu.Unlock()
	require.NotEmpty(t, received.traces)
	require.NotEmpty(t, received.metrics)
	spans := decodedSemanticSpans(t, received.traces)
	require.Len(t, spans, 3)
	var host, known, unknown *tracepb.Span
	for _, span := range spans {
		switch {
		case span.GetName() == "origin":
			host = span
		case wireAttribute(span.GetAttributes(), genai.InputTokens) != nil:
			known = span
		default:
			unknown = span
		}
	}
	require.NotNil(t, host)
	require.NotNil(t, known)
	require.NotNil(t, unknown)
	require.Equal(t, host.GetTraceId(), known.GetTraceId())
	require.Equal(t, host.GetSpanId(), known.GetParentSpanId())
	require.Empty(t, unknown.GetParentSpanId())
	require.NotEqual(t, host.GetTraceId(), unknown.GetTraceId())
	require.Len(t, unknown.GetLinks(), 1)
	require.Equal(t, host.GetTraceId(), unknown.GetLinks()[0].GetTraceId())
	require.Equal(t, host.GetSpanId(), unknown.GetLinks()[0].GetSpanId())
	require.Equal(t, tracepb.Span_SPAN_KIND_CLIENT, unknown.GetKind())
	require.Equal(t, tracepb.Status_STATUS_CODE_ERROR, unknown.GetStatus().GetCode())
	require.Equal(t, "cancelled", unknown.GetStatus().GetMessage())
	require.Equal(t, "cancelled", wireAttribute(unknown.GetAttributes(), genai.ErrorType).GetStringValue())
	require.Equal(t, "error", wireAttribute(unknown.GetAttributes(), genai.OperationStatus).GetStringValue())
	require.Equal(t, tracepb.Status_STATUS_CODE_OK, known.GetStatus().GetCode())
	require.NotNil(t, wireAttribute(known.GetAttributes(), genai.InputTokens))
	require.IsType(t, &commonpb.AnyValue_IntValue{}, wireAttribute(known.GetAttributes(), genai.InputTokens).GetValue())
	require.Equal(t, int64(0), wireAttribute(known.GetAttributes(), genai.InputTokens).GetIntValue())
	for _, key := range []string{genai.InputTokens, genai.OutputTokens, genai.UsageCost} {
		require.Nil(t, wireAttribute(unknown.GetAttributes(), key))
	}
	require.Contains(t, wireAttribute(known.GetAttributes(), genai.InputMessages).GetStringValue(), "[redacted]")
	require.Contains(t, wireAttribute(unknown.GetAttributes(), genai.RequestModel).GetStringValue(), "sha256:")
	instruments := decodedSemanticMetrics(t, received.metrics)
	assertWireHistogram(t, instruments, genai.InputTokenUsageMetricName, "{token}", 0, "measured")
	assertWireHistogram(t, instruments, genai.OutputTokenUsageMetricName, "{token}", 4, "provider_reported")
	assertWireHistogram(t, instruments, genai.ObservedOperationDurationMetricName, "s", fixtureDuration.Seconds(), "")
	cost := instruments[genai.CostMetricName]
	require.NotNil(t, cost)
	sum := cost.GetSum()
	require.NotNil(t, sum)
	require.True(t, sum.GetIsMonotonic())
	require.Equal(
		t,
		metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
		sum.GetAggregationTemporality(),
	)
	require.Len(t, sum.GetDataPoints(), 1)
	point := sum.GetDataPoints()[0]
	require.IsType(t, &metricpb.NumberDataPoint_AsDouble{}, point.GetValue())
	require.InDelta(t, fixtureCost, point.GetAsDouble(), 1e-9)
	require.Equal(t, "USD", wireAttribute(point.GetAttributes(), genai.CostCurrency).GetStringValue())
	require.Equal(t, "provider_reported", wireAttribute(point.GetAttributes(), "metry.usage.source").GetStringValue())
}

func decodedSemanticSpans(t *testing.T, raws [][]byte) []*tracepb.Span {
	t.Helper()
	var spans []*tracepb.Span
	for _, raw := range raws {
		require.False(t, bytes.Contains(raw, []byte(semanticSentinel)), "raw sentinel in trace protobuf")
		request := &collectortrace.ExportTraceServiceRequest{}
		require.NoError(t, proto.Unmarshal(raw, request))
		for _, resource := range request.GetResourceSpans() {
			require.Equal(t, resourceSchema, resource.GetSchemaUrl())
			assertWireResource(t, resource.GetResource().GetAttributes())
			for _, scope := range resource.GetScopeSpans() {
				if scope.GetScope().GetName() == semanticScope {
					assertWireScope(t, scope.GetScope(), scope.GetSchemaUrl())
				} else {
					require.Equal(t, "semantic-host", scope.GetScope().GetName())
					require.Len(t, scope.GetSpans(), 1)
					require.Equal(t, "origin", scope.GetSpans()[0].GetName())
				}
				spans = append(spans, scope.GetSpans()...)
			}
		}
	}
	return spans
}

func decodedSemanticMetrics(t *testing.T, raws [][]byte) map[string]*metricpb.Metric {
	t.Helper()
	instruments := map[string]*metricpb.Metric{}
	for _, raw := range raws {
		require.False(t, bytes.Contains(raw, []byte(semanticSentinel)), "raw sentinel in metric protobuf")
		request := &collectormetric.ExportMetricsServiceRequest{}
		require.NoError(t, proto.Unmarshal(raw, request))
		for _, resource := range request.GetResourceMetrics() {
			require.Equal(t, resourceSchema, resource.GetSchemaUrl())
			assertWireResource(t, resource.GetResource().GetAttributes())
			for _, scope := range resource.GetScopeMetrics() {
				assertWireScope(t, scope.GetScope(), scope.GetSchemaUrl())
				for _, instrument := range scope.GetMetrics() {
					require.NotContains(t, instruments, instrument.GetName(), "duplicate export before assertions")
					instruments[instrument.GetName()] = instrument
				}
			}
		}
	}
	return instruments
}

func assertWireResource(t *testing.T, attrs []*commonpb.KeyValue) {
	t.Helper()
	require.Equal(t, "semantic-consumer", wireAttribute(attrs, "service.name").GetStringValue())
	require.Equal(t, "fixture-v1", wireAttribute(attrs, "service.version").GetStringValue())
}
func assertWireScope(t *testing.T, scope *commonpb.InstrumentationScope, schema string) {
	t.Helper()
	require.NotNil(t, scope)
	require.Equal(t, semanticScope, scope.GetName())
	require.Equal(t, semanticVersion, scope.GetVersion())
	require.Equal(t, semanticSchema, schema)
}
func wireAttribute(attrs []*commonpb.KeyValue, key string) *commonpb.AnyValue {
	for _, attr := range attrs {
		if attr.GetKey() == key {
			return attr.GetValue()
		}
	}
	return nil
}

func assertWireHistogram(
	t *testing.T,
	instruments map[string]*metricpb.Metric,
	name, unit string,
	want float64,
	source string,
) {
	t.Helper()
	instrument := instruments[name]
	require.NotNil(t, instrument)
	require.Equal(t, unit, instrument.GetUnit())
	histogram := instrument.GetHistogram()
	require.NotNil(t, histogram)
	require.Equal(
		t,
		metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
		histogram.GetAggregationTemporality(),
	)
	require.Len(t, histogram.GetDataPoints(), 1)
	point := histogram.GetDataPoints()[0]
	require.Equal(t, uint64(1), point.GetCount())
	require.NotNil(t, point.Sum)
	require.InDelta(t, want, point.GetSum(), 1e-9)
	var count uint64
	for _, bucket := range point.GetBucketCounts() {
		count += bucket
	}
	require.Equal(t, point.GetCount(), count)
	require.Len(t, point.GetBucketCounts(), len(point.GetExplicitBounds())+1)
	if source != "" {
		require.Equal(t, source, wireAttribute(point.GetAttributes(), "metry.usage.source").GetStringValue())
	}
}
