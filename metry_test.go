package metry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/internal/genaitest"
	"github.com/skosovsky/metry/testutil"
)

func TestInit_ValidOptions_Succeeds(t *testing.T) {
	ctx := context.Background()
	genaitest.InstallDefaultTrackerForTest(t, nil)
	shutdown, err := Init(ctx, WithServiceName("test-svc"), WithServiceVersion("1.0.0"), WithEnvironment("test"))
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	t.Cleanup(func() { _ = shutdown(ctx) })

	require.NotNil(t, otel.Tracer("metry"))
	require.NotNil(t, otel.Meter("metry"))
	require.NotNil(t, genai.Default())
}

func TestInit_MissingServiceName_ReturnsError(t *testing.T) {
	ctx := context.Background()
	genaitest.InstallDefaultTrackerForTest(t, nil)
	shutdown, err := Init(ctx, WithEnvironment("test"))
	require.ErrorIs(t, err, ErrServiceNameRequired)
	require.Nil(t, shutdown)
}

func TestInit_WithGenAIConfig_InstallsDefaultTracker(t *testing.T) {
	ctx := context.Background()
	genaitest.InstallDefaultTrackerForTest(t, nil)
	shutdown, err := Init(ctx,
		WithServiceName("test-svc"),
		WithGenAIConfig(
			genai.WithRecordPayloads(true),
			genai.WithMaxContextLength(96),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdown(ctx) })

	value, ok := recordInputMessagesAttribute(t, strings.Repeat("a", 256))
	require.True(t, ok)
	require.LessOrEqual(t, len(value), 96)
	require.True(t, json.Valid([]byte(value)))
}

func TestShutdown_OldSessionDoesNotResetNewDefaultTracker(t *testing.T) {
	ctx := context.Background()
	genaitest.InstallDefaultTrackerForTest(t, nil)

	shutdown1, err := Init(ctx,
		WithServiceName("test-svc-1"),
		WithGenAIConfig(genai.WithRecordPayloads(true), genai.WithMaxContextLength(128)),
	)
	require.NoError(t, err)

	shutdown2, err := Init(ctx,
		WithServiceName("test-svc-2"),
		WithGenAIConfig(genai.WithRecordPayloads(false), genai.WithMaxContextLength(32)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdown2(ctx) })

	require.NoError(t, shutdown1(ctx))

	_, ok := recordInputMessagesAttribute(t, "should-stay-hidden")
	require.False(t, ok)
}

func TestInit_DoubleInitWithMetricExporter_ReplacesDefaultTracker(t *testing.T) {
	ctx := context.Background()
	genaitest.InstallDefaultTrackerForTest(t, nil)
	firstMetrics := testutil.NewInMemoryMetricExporter()
	shutdown1, err := Init(ctx,
		WithServiceName("test-svc-1"),
		WithMetricExporter(firstMetrics.Exporter()),
		WithGenAIConfig(genai.WithRecordPayloads(true), genai.WithMaxContextLength(128)),
	)
	require.NoError(t, err)

	secondMetrics := testutil.NewInMemoryMetricExporter()
	shutdown2, err := Init(ctx,
		WithServiceName("test-svc-2"),
		WithMetricExporter(secondMetrics.Exporter()),
		WithGenAIConfig(genai.WithRecordPayloads(false), genai.WithMaxContextLength(32)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = shutdown2(ctx)
		_ = shutdown1(ctx)
	})

	_, ok := recordInputMessagesAttribute(t, "should-be-hidden")
	require.False(t, ok)
}

func recordInputMessagesAttribute(t *testing.T, text string) (string, bool) {
	t.Helper()

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := tp.Tracer("config-test").Start(context.Background(), "span")
	genai.RecordInteraction(context.Background(), span, genai.GenAIMeta{
		Provider:      "openai",
		Operation:     "chat",
		RequestModel:  "gpt-4o-mini",
		ResponseModel: "gpt-4o-mini",
		Duration:      time.Second,
	}, genai.GenAIPayload{
		InputMessages: []genai.GenAIMessage{{
			Role: "user",
			Parts: []genai.GenAIContentPart{{
				Type:    "text",
				Content: text,
			}},
		}},
	}, genai.GenAIUsage{})
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	value, ok := attrs.Value(genai.InputMessagesKey)
	if !ok {
		return "", false
	}
	return value.AsString(), true
}
