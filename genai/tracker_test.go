package genai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewTracker_NilMeter_IsAllowed(t *testing.T) {
	tracker, err := NewTracker(nil)
	require.NoError(t, err)
	require.NotNil(t, tracker)
	assert.Nil(t, tracker.metrics)
	assert.False(t, tracker.cfg.RecordPayloads())
	assert.Equal(t, defaultMaxContextLength, tracker.cfg.MaxContextLength())
	require.NotNil(t, tracker.tracer)
}

func TestNewTrackerWithTracer_UsesExplicitTracerAndKeepsTrackersIsolated(t *testing.T) {
	reader1 := sdkmetric.NewManualReader()
	mp1 := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader1))
	t.Cleanup(func() { _ = mp1.Shutdown(context.Background()) })
	traceExporter1 := tracetest.NewInMemoryExporter()
	tp1 := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExporter1))
	t.Cleanup(func() { _ = tp1.Shutdown(context.Background()) })

	reader2 := sdkmetric.NewManualReader()
	mp2 := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader2))
	t.Cleanup(func() { _ = mp2.Shutdown(context.Background()) })
	traceExporter2 := tracetest.NewInMemoryExporter()
	tp2 := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExporter2))
	t.Cleanup(func() { _ = tp2.Shutdown(context.Background()) })

	tracker1, err := NewTrackerWithTracer(
		mp1.Meter("tracker-1"),
		tp1.Tracer("tracker-1"),
		WithRecordPayloads(true),
		WithMaxContextLength(96),
	)
	require.NoError(t, err)
	tracker2, err := NewTrackerWithTracer(
		mp2.Meter("tracker-2"),
		tp2.Tracer("tracker-2"),
		WithRecordPayloads(false),
		WithMaxContextLength(32),
	)
	require.NoError(t, err)

	toolJSON := `{"prompt":"` + strings.Repeat("a", 512) + `"}`

	ctx1, toolSpan1 := tracker1.StartToolSpan(context.Background(), "search", "call-1", toolJSON)
	tracker1.RecordInteraction(ctx1, toolSpan1, testMeta(), testPayload(), GenAIUsage{
		InputTokens:           12,
		OutputTokens:          6,
		CacheReadInputTokens:  3,
		ReasoningOutputTokens: 2,
	})
	tracker1.RecordToolResult(toolSpan1, `{"result":"`+strings.Repeat("b", 512)+`"}`, false)
	toolSpan1.End()

	ctx2, toolSpan2 := tracker2.StartToolSpan(context.Background(), "search", "call-2", toolJSON)
	tracker2.RecordInteraction(ctx2, toolSpan2, testMeta(), testPayload(), GenAIUsage{
		InputTokens:  4,
		OutputTokens: 2,
	})
	tracker2.RecordToolResult(toolSpan2, `{"result":"ok"}`, false)
	toolSpan2.End()

	spans1 := traceExporter1.GetSpans()
	require.Len(t, spans1, 1)
	attrs1 := spanAttributes(spans1[0].Attributes)
	inputMessages1, ok := attrs1[InputMessagesKey]
	require.True(t, ok)
	require.True(t, json.Valid([]byte(inputMessages1.AsString())))
	toolArgs1, ok := attrs1[ToolCallArgumentsKey]
	require.True(t, ok)
	require.True(t, json.Valid([]byte(toolArgs1.AsString())))

	spans2 := traceExporter2.GetSpans()
	require.Len(t, spans2, 1)
	attrs2 := spanAttributes(spans2[0].Attributes)
	_, ok = attrs2[InputMessagesKey]
	require.False(t, ok)
	toolArgs2, ok := attrs2[ToolCallArgumentsKey]
	require.True(t, ok)
	require.True(t, json.Valid([]byte(toolArgs2.AsString())))

	rm1 := collectMetrics(t, reader1)
	assert.InDelta(t, 12, int64HistogramSumByTokenType(t, rm1, TokenUsageMetricName, TokenTypeInput), 1e-9)
	assert.InDelta(t, 6, int64HistogramSumByTokenType(t, rm1, TokenUsageMetricName, TokenTypeOutput), 1e-9)
	assert.InDelta(
		t,
		3,
		int64HistogramSumByTokenType(t, rm1, TokenComponentUsageMetricName, TokenTypeInputCacheRead),
		1e-9,
	)
	assert.InDelta(
		t,
		2,
		int64HistogramSumByTokenType(t, rm1, TokenComponentUsageMetricName, TokenTypeOutputReasoning),
		1e-9,
	)

	rm2 := collectMetrics(t, reader2)
	assert.InDelta(t, 4, int64HistogramSumByTokenType(t, rm2, TokenUsageMetricName, TokenTypeInput), 1e-9)
	assert.InDelta(t, 2, int64HistogramSumByTokenType(t, rm2, TokenUsageMetricName, TokenTypeOutput), 1e-9)
	assertMetricAbsent(t, rm2, TokenComponentUsageMetricName)
}

func spanAttributes(attrs []attribute.KeyValue) map[attribute.Key]attribute.Value {
	set := make(map[attribute.Key]attribute.Value, len(attrs))
	for _, attr := range attrs {
		set[attr.Key] = attr.Value
	}
	return set
}
