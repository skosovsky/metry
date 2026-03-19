package metry

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/testutil"
)

func TestInit_ValidOptions_Succeeds(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, WithServiceName("test-svc"), WithServiceVersion("1.0.0"), WithEnvironment("test"))
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	require.NotNil(t, otel.Tracer("metry"))
	require.NotNil(t, otel.Meter("metry"))
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

	require.NotNil(t, otel.Tracer("metry"))
}

func TestInit_ZeroTraceRatio(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, WithServiceName("test-svc"), WithTraceRatio(0))
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	require.NotNil(t, otel.Tracer("metry"))
}

func TestShutdown_Idempotent(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, WithServiceName("test-svc"))
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	require.NoError(t, shutdown(ctx))
	require.NoError(t, shutdown(ctx))
}

func TestInit_WithExporter_Succeeds(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx,
		WithServiceName("test-svc"),
		WithExporter(tracetest.NewInMemoryExporter()),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	require.NotNil(t, otel.Tracer("metry"))
}

func TestInit_WithMetricExporter_Succeeds(t *testing.T) {
	ctx := context.Background()
	metrics := testutil.NewInMemoryMetricExporter()
	shutdown, err := Init(ctx,
		WithServiceName("test-svc"),
		WithMetricExporter(metrics.Exporter()),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})
}

func TestInit_DoubleInitWithMetricExporter_RestoresPreviousProvidersOnFailure(t *testing.T) {
	ctx := context.Background()
	firstMetrics := testutil.NewInMemoryMetricExporter()
	firstTrace := tracetest.NewInMemoryExporter()
	shutdown, err := Init(ctx,
		WithServiceName("test-svc"),
		WithExporter(firstTrace),
		WithMetricExporter(firstMetrics.Exporter()),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	prevTracer := otel.GetTracerProvider()
	prevMeter := otel.GetMeterProvider()

	secondMetrics := testutil.NewInMemoryMetricExporter()
	secondTrace := tracetest.NewInMemoryExporter()
	shutdown2, err := Init(ctx,
		WithServiceName("test-svc-2"),
		WithExporter(secondTrace),
		WithMetricExporter(secondMetrics.Exporter()),
	)
	require.Error(t, err)
	require.Nil(t, shutdown2)
	require.Same(t, prevTracer, otel.GetTracerProvider())
	require.Same(t, prevMeter, otel.GetMeterProvider())
}

func TestInit_DoubleInitFailure_RestoresPreviousGenAIConfig(t *testing.T) {
	ctx := context.Background()
	firstMetrics := testutil.NewInMemoryMetricExporter()
	firstTrace := tracetest.NewInMemoryExporter()
	shutdown, err := Init(ctx,
		WithServiceName("test-svc"),
		WithExporter(firstTrace),
		WithMetricExporter(firstMetrics.Exporter()),
		WithRecordPayloads(true),
		WithMaxGenAIContextLength(32),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() {
		_ = shutdown(ctx)
	})

	secondMetrics := testutil.NewInMemoryMetricExporter()
	secondTrace := tracetest.NewInMemoryExporter()
	shutdown2, err := Init(ctx,
		WithServiceName("test-svc-2"),
		WithExporter(secondTrace),
		WithMetricExporter(secondMetrics.Exporter()),
		WithRecordPayloads(false),
		WithMaxGenAIContextLength(4),
	)
	require.Error(t, err)
	require.Nil(t, shutdown2)

	prompt, ok := recordPromptAttribute(t, strings.Repeat("a", 128))
	require.True(t, ok, "failed re-init must preserve payload recording from the active session")
	require.LessOrEqual(t, len(prompt), 32, "failed re-init must preserve the active truncation limit")
	require.Less(t, len(prompt), 128, "prompt must still be truncated by the active session config")
}

func TestShutdown_OldSessionDoesNotResetNewGenAIConfig(t *testing.T) {
	ctx := context.Background()

	shutdown1, err := Init(ctx,
		WithServiceName("test-svc-1"),
		WithRecordPayloads(true),
		WithMaxGenAIContextLength(32),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown1)

	shutdown2, err := Init(ctx,
		WithServiceName("test-svc-2"),
		WithRecordPayloads(false),
		WithMaxGenAIContextLength(8),
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown2)
	t.Cleanup(func() {
		_ = shutdown2(ctx)
	})

	require.NoError(t, shutdown1(ctx))

	_, ok := recordPromptAttribute(t, "should-stay-hidden")
	require.False(t, ok, "shutdown of an older session must not reset the active session config")
}

func recordPromptAttribute(t *testing.T, prompt string) (string, bool) {
	t.Helper()

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	_, span := tp.Tracer("config-test").Start(context.Background(), "span")
	genai.RecordInteraction(context.Background(), span, genai.GenAIPayload{Prompt: prompt}, genai.GenAIUsage{})
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	value, ok := attrs.Value(genai.PromptKey)
	if !ok {
		return "", false
	}
	return value.AsString(), true
}
