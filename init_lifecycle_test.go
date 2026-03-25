package metry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/internal/genaitest"
	"github.com/skosovsky/metry/testutil"
)

func TestInit_Shutdown_Init_DefaultTrackerAndMetricsWork(t *testing.T) {
	ctx := context.Background()
	genaitest.InstallDefaultTrackerForTest(t, nil)
	mem := testutil.NewInMemoryMetricExporter()
	tr := testutil.NewInMemoryTraceExporter()

	shutdown1, err := Init(ctx,
		WithServiceName("test-svc"),
		WithExporter(tr.SpanExporter()),
		WithMetricExporter(mem.Exporter()),
		WithGenAIConfig(genai.WithRecordPayloads(true), genai.WithMaxContextLength(64)),
	)
	require.NoError(t, err)

	_, span1 := otel.Tracer("metry").Start(ctx, "span-1")
	genai.RecordInteraction(ctx, span1, testMeta(), genai.GenAIPayload{
		InputMessages: []genai.GenAIMessage{{
			Role: "user",
			Parts: []genai.GenAIContentPart{{
				Type:    "text",
				Content: "1234567890",
			}},
		}},
	}, genai.GenAIUsage{})
	span1.End()
	require.NoError(t, shutdown1(ctx))

	tr.Reset()
	mem.Reset()

	shutdown2, err := Init(ctx,
		WithServiceName("test-svc"),
		WithExporter(tr.SpanExporter()),
		WithMetricExporter(mem.Exporter()),
	)
	require.NoError(t, err)

	_, span2 := otel.Tracer("metry").Start(ctx, "span-2")
	genai.RecordInteraction(ctx, span2, testMeta(), genai.GenAIPayload{
		InputMessages: []genai.GenAIMessage{{
			Role: "user",
			Parts: []genai.GenAIContentPart{{
				Type:    "text",
				Content: "should-not-export",
			}},
		}},
	}, genai.GenAIUsage{
		InputTokens:  1,
		OutputTokens: 2,
		Cost:         0.001,
	})
	span2.End()

	require.NoError(t, shutdown2(ctx))
	rm := mem.LastResourceMetrics()
	require.NotNil(t, rm)

	assertMetricPresent(t, *rm, genai.TokenUsageMetricName)
	assertMetricAbsent(t, *rm, genai.TokenComponentUsageMetricName)
	assertMetricPresent(t, *rm, genai.OperationDurationMetricName)
	assertMetricPresent(t, *rm, genai.CostMetricName)

	tr.Reset()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(tr.SpanExporter()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span3 := tp.Tracer("post-shutdown").Start(ctx, "span-3")
	genai.RecordInteraction(ctx, span3, testMeta(), genai.GenAIPayload{
		InputMessages: []genai.GenAIMessage{{
			Role: "user",
			Parts: []genai.GenAIContentPart{{
				Type:    "text",
				Content: "still-hidden",
			}},
		}},
	}, genai.GenAIUsage{})
	span3.End()

	spans := tr.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	_, ok := attrs.Value(genai.InputMessagesKey)
	require.False(t, ok)
}

func TestInit_Shutdown_RestoresPreviousGlobalOTelState(t *testing.T) {
	ctx := context.Background()
	previousTracker, err := genai.NewTracker(nil, genai.WithRecordPayloads(true))
	require.NoError(t, err)
	genaitest.InstallDefaultTrackerForTest(t, previousTracker)

	prevTracerProvider := sdktrace.NewTracerProvider()
	prevMeterReader := sdkmetric.NewManualReader()
	prevMeterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(prevMeterReader))
	prevPropagator := &testPropagator{name: "previous"}
	installGlobalOTelStateForTest(t, prevTracerProvider, prevMeterProvider, prevPropagator)
	t.Cleanup(func() {
		_ = prevTracerProvider.Shutdown(context.Background())
		_ = prevMeterProvider.Shutdown(context.Background())
	})

	traceExporter := testutil.NewInMemoryTraceExporter()
	metricExporter := testutil.NewInMemoryMetricExporter()
	shutdown, err := Init(ctx,
		WithServiceName("test-svc"),
		WithExporter(traceExporter.SpanExporter()),
		WithMetricExporter(metricExporter.Exporter()),
	)
	require.NoError(t, err)
	require.NotSame(t, prevTracerProvider, otel.GetTracerProvider())
	require.NotSame(t, prevMeterProvider, otel.GetMeterProvider())
	require.NotSame(t, prevPropagator, otel.GetTextMapPropagator())
	require.NotSame(t, previousTracker, genai.Default())

	require.NoError(t, shutdown(ctx))
	require.Same(t, prevTracerProvider, otel.GetTracerProvider())
	require.Same(t, prevMeterProvider, otel.GetMeterProvider())
	require.Same(t, prevPropagator, otel.GetTextMapPropagator())
	require.Same(t, previousTracker, genai.Default())
}

func TestShutdown_OldSessionDoesNotResetNewGlobalOTelState(t *testing.T) {
	ctx := context.Background()
	previousTracker, err := genai.NewTracker(nil, genai.WithRecordPayloads(true), genai.WithMaxContextLength(256))
	require.NoError(t, err)
	genaitest.InstallDefaultTrackerForTest(t, previousTracker)

	prevTracerProvider := sdktrace.NewTracerProvider()
	prevMeterReader := sdkmetric.NewManualReader()
	prevMeterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(prevMeterReader))
	prevPropagator := &testPropagator{name: "previous"}
	installGlobalOTelStateForTest(t, prevTracerProvider, prevMeterProvider, prevPropagator)
	t.Cleanup(func() {
		_ = prevTracerProvider.Shutdown(context.Background())
		_ = prevMeterProvider.Shutdown(context.Background())
	})

	traceExporter1 := testutil.NewInMemoryTraceExporter()
	metricExporter1 := testutil.NewInMemoryMetricExporter()
	shutdown1, err := Init(ctx,
		WithServiceName("test-svc-1"),
		WithExporter(traceExporter1.SpanExporter()),
		WithMetricExporter(metricExporter1.Exporter()),
		WithGenAIConfig(genai.WithRecordPayloads(true), genai.WithMaxContextLength(128)),
	)
	require.NoError(t, err)
	tracerAfterInit1 := otel.GetTracerProvider()
	meterAfterInit1 := otel.GetMeterProvider()
	propagatorAfterInit1 := otel.GetTextMapPropagator()
	trackerAfterInit1 := genai.Default()

	traceExporter2 := testutil.NewInMemoryTraceExporter()
	metricExporter2 := testutil.NewInMemoryMetricExporter()
	shutdown2, err := Init(ctx,
		WithServiceName("test-svc-2"),
		WithExporter(traceExporter2.SpanExporter()),
		WithMetricExporter(metricExporter2.Exporter()),
		WithGenAIConfig(genai.WithRecordPayloads(false), genai.WithMaxContextLength(32)),
	)
	require.NoError(t, err)
	tracerAfterInit2 := otel.GetTracerProvider()
	meterAfterInit2 := otel.GetMeterProvider()
	propagatorAfterInit2 := otel.GetTextMapPropagator()
	trackerAfterInit2 := genai.Default()

	require.NotSame(t, tracerAfterInit1, tracerAfterInit2)
	require.NotSame(t, meterAfterInit1, meterAfterInit2)
	require.NotSame(t, propagatorAfterInit1, propagatorAfterInit2)
	require.NotSame(t, trackerAfterInit1, trackerAfterInit2)

	require.NoError(t, shutdown1(ctx))
	require.Same(t, tracerAfterInit2, otel.GetTracerProvider())
	require.Same(t, meterAfterInit2, otel.GetMeterProvider())
	require.Same(t, propagatorAfterInit2, otel.GetTextMapPropagator())
	require.Same(t, trackerAfterInit2, genai.Default())

	require.NoError(t, shutdown2(ctx))
	require.Same(t, prevTracerProvider, otel.GetTracerProvider())
	require.Same(t, prevMeterProvider, otel.GetMeterProvider())
	require.Same(t, prevPropagator, otel.GetTextMapPropagator())
	require.Same(t, previousTracker, genai.Default())
}

func TestShutdown_MetricSessionDoesNotLeakIntoNewNoMetricSession(t *testing.T) {
	ctx := context.Background()
	genaitest.InstallDefaultTrackerForTest(t, nil)

	firstTrace := testutil.NewInMemoryTraceExporter()
	firstMetrics := testutil.NewInMemoryMetricExporter()
	shutdown1, err := Init(ctx,
		WithServiceName("test-svc-1"),
		WithExporter(firstTrace.SpanExporter()),
		WithMetricExporter(firstMetrics.Exporter()),
		WithGenAIConfig(genai.WithRecordPayloads(true), genai.WithMaxContextLength(128)),
	)
	require.NoError(t, err)
	meterAfterInit1 := otel.GetMeterProvider()

	secondTrace := testutil.NewInMemoryTraceExporter()
	shutdown2, err := Init(ctx,
		WithServiceName("test-svc-2"),
		WithExporter(secondTrace.SpanExporter()),
		WithGenAIConfig(genai.WithRecordPayloads(false), genai.WithMaxContextLength(32)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = shutdown2(ctx)
		_ = shutdown1(ctx)
	})

	meterAfterInit2 := otel.GetMeterProvider()
	require.NotEqual(t, meterAfterInit1, meterAfterInit2)

	firstMetrics.Reset()
	_, overlapSpan := otel.Tracer("metry").Start(ctx, "span-no-metrics-overlap")
	genai.RecordInteraction(ctx, overlapSpan, testMeta(), genai.GenAIPayload{
		InputMessages: []genai.GenAIMessage{{
			Role: "user",
			Parts: []genai.GenAIContentPart{{
				Type:    "text",
				Content: "active-before-shutdown",
			}},
		}},
	}, genai.GenAIUsage{
		InputTokens:  3,
		OutputTokens: 1,
	})
	overlapSpan.End()
	forceFlushTracerProvider(ctx, t)

	require.Len(t, secondTrace.GetSpans(), 1)
	require.Zero(t, firstMetrics.GetMetrics())
	if rm := firstMetrics.LastResourceMetrics(); rm != nil {
		assertMetricAbsent(t, *rm, genai.TokenUsageMetricName)
		assertMetricAbsent(t, *rm, genai.CostMetricName)
	}

	secondTrace.Reset()
	require.NoError(t, shutdown1(ctx))
	require.Equal(t, meterAfterInit2, otel.GetMeterProvider())
	firstMetrics.Reset()

	_, span := otel.Tracer("metry").Start(ctx, "span-no-metrics")
	genai.RecordInteraction(ctx, span, testMeta(), genai.GenAIPayload{
		InputMessages: []genai.GenAIMessage{{
			Role: "user",
			Parts: []genai.GenAIContentPart{{
				Type:    "text",
				Content: "still-active",
			}},
		}},
	}, genai.GenAIUsage{
		InputTokens:  5,
		OutputTokens: 2,
	})
	span.End()
	forceFlushTracerProvider(ctx, t)

	require.Len(t, secondTrace.GetSpans(), 1)
	require.NoError(t, shutdown2(ctx))
	require.Zero(t, firstMetrics.GetMetrics())
	if rm := firstMetrics.LastResourceMetrics(); rm != nil {
		assertMetricAbsent(t, *rm, genai.TokenUsageMetricName)
		assertMetricAbsent(t, *rm, genai.CostMetricName)
	}
}

func TestShutdown_NoMetricSessionDoesNotResetNewMetricSession(t *testing.T) {
	ctx := context.Background()
	genaitest.InstallDefaultTrackerForTest(t, nil)

	firstTrace := testutil.NewInMemoryTraceExporter()
	shutdown1, err := Init(ctx,
		WithServiceName("test-svc-1"),
		WithExporter(firstTrace.SpanExporter()),
		WithGenAIConfig(genai.WithRecordPayloads(true), genai.WithMaxContextLength(128)),
	)
	require.NoError(t, err)
	meterAfterInit1 := otel.GetMeterProvider()

	secondTrace := testutil.NewInMemoryTraceExporter()
	secondMetrics := testutil.NewInMemoryMetricExporter()
	shutdown2, err := Init(ctx,
		WithServiceName("test-svc-2"),
		WithExporter(secondTrace.SpanExporter()),
		WithMetricExporter(secondMetrics.Exporter()),
		WithGenAIConfig(genai.WithRecordPayloads(false), genai.WithMaxContextLength(32)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = shutdown2(ctx)
		_ = shutdown1(ctx)
	})

	meterAfterInit2 := otel.GetMeterProvider()
	require.NotEqual(t, meterAfterInit1, meterAfterInit2)

	require.NoError(t, shutdown1(ctx))
	require.Equal(t, meterAfterInit2, otel.GetMeterProvider())

	_, span := otel.Tracer("metry").Start(ctx, "span-metrics-still-active")
	genai.RecordInteraction(ctx, span, testMeta(), genai.GenAIPayload{}, genai.GenAIUsage{
		InputTokens:  5,
		OutputTokens: 2,
		Cost:         0.01,
	})
	span.End()

	require.NoError(t, shutdown2(ctx))
	rm := secondMetrics.LastResourceMetrics()
	require.NotNil(t, rm)
	assertMetricPresent(t, *rm, genai.TokenUsageMetricName)
	assertMetricPresent(t, *rm, genai.CostMetricName)
}

func TestShutdown_PostShutdownPackageLevelHelpersAreTracingNoopWithoutPreexistingDefaultTracker(t *testing.T) {
	ctx := context.Background()
	genaitest.InstallDefaultTrackerForTest(t, nil)

	ambientTrace := testutil.NewInMemoryTraceExporter()
	ambientTracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(ambientTrace.SpanExporter()))
	t.Cleanup(func() { _ = ambientTracerProvider.Shutdown(context.Background()) })
	ambientMeterProvider := noopmetric.NewMeterProvider()
	ambientPropagator := &testPropagator{name: "ambient"}
	installGlobalOTelStateForTest(t, ambientTracerProvider, ambientMeterProvider, ambientPropagator)

	shutdown, err := Init(ctx, WithServiceName("test-svc"))
	require.NoError(t, err)
	require.NoError(t, shutdown(ctx))

	_, toolSpan := genai.StartToolSpan(ctx, "search", "call-1", `{"q":"noop"}`)
	toolSpan.End()
	traceID := trace.TraceID{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}
	require.NoError(t, genai.RecordAsyncFeedback(ctx, traceID.String(), 0.5, "noop"))
	require.Empty(t, ambientTrace.GetSpans())
}

func testMeta() genai.GenAIMeta {
	return genai.GenAIMeta{
		Provider:      "openai",
		Operation:     "chat",
		RequestModel:  "gpt-4o-mini",
		ResponseModel: "gpt-4o-mini",
		Duration:      200 * time.Millisecond,
	}
}

func assertMetricPresent(t *testing.T, rm metricdata.ResourceMetrics, name string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return
			}
		}
	}
	t.Fatalf("metric %q not found", name)
}

func assertMetricAbsent(t *testing.T, rm metricdata.ResourceMetrics, name string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				t.Fatalf("metric %q unexpectedly found", name)
			}
		}
	}
}

func installGlobalOTelStateForTest(
	t testing.TB,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	propagator propagation.TextMapPropagator,
) {
	t.Helper()

	previousTracerProvider := otel.GetTracerProvider()
	previousMeterProvider := otel.GetMeterProvider()
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTracerProvider)
		otel.SetMeterProvider(previousMeterProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagator)
}

func forceFlushTracerProvider(ctx context.Context, t testing.TB) {
	t.Helper()

	provider, ok := otel.GetTracerProvider().(interface{ ForceFlush(context.Context) error })
	require.True(t, ok)
	require.NoError(t, provider.ForceFlush(ctx))
}

type testPropagator struct {
	name string
}

func (p *testPropagator) Inject(context.Context, propagation.TextMapCarrier) {}

func (p *testPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	_ = carrier
	return ctx
}

func (p *testPropagator) Fields() []string {
	return []string{p.name}
}
