package genai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestRecordInteraction_WithPayloadAndUsage_SetsAttributes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("genai-test"),
		tp.Tracer("genai-test"),
		WithRecordPayloads(true),
	)
	require.NoError(t, err)

	ctx := context.Background()
	_, span := tp.Tracer("genai-test").Start(ctx, "span")
	tracker.RecordInteraction(ctx, span, testMeta(), testPayload(), Usage{
		InputTokens:           10,
		OutputTokens:          20,
		ReasoningOutputTokens: 4,
		Cost:                  0.25,
		Currency:              "CREDITS",
	})
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, "openai", mustStringAttr(t, attrs, ProviderNameKey))
	assert.Equal(t, "chat", mustStringAttr(t, attrs, OperationNameKey))
	assert.Equal(t, int64(10), mustIntAttr(t, attrs, InputTokensKey))
	assert.Equal(t, int64(20), mustIntAttr(t, attrs, OutputTokensKey))
	assert.Equal(t, int64(4), mustIntAttr(t, attrs, UsageReasoningOutputTokensKey))
	assert.InDelta(t, 0.25, mustFloatAttr(t, attrs, UsageCostKey), 1e-9)
	assert.Equal(t, "CREDITS", mustStringAttr(t, attrs, CostCurrencyKey))
}

func TestRecordInteraction_TruncatesPayloadString(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("genai-test"),
		tp.Tracer("genai-test"),
		WithRecordPayloads(true),
		WithMaxContextLength(96),
	)
	require.NoError(t, err)

	payload := Payload{
		InputMessages: []Message{{
			Role: "user",
			Parts: []ContentPart{{
				Type:    "text",
				Content: strings.Repeat("a", 2048),
			}},
		}},
	}

	_, span := tp.Tracer("genai-test").Start(context.Background(), "span")
	tracker.RecordInteraction(context.Background(), span, testMeta(), payload, Usage{})
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	value := mustStringAttr(t, attrs, InputMessagesKey)
	assert.LessOrEqual(t, len(value), 96)
	assert.True(t, utf8.ValidString(value))
}

func TestStartToolSpan_AndRecordToolResult_SetToolAttributes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("genai-test"),
		tp.Tracer("genai-test"),
		WithMaxContextLength(64),
	)
	require.NoError(t, err)

	_, span := tracker.StartToolSpan(context.Background(), "search", "call-1", `{"q":`)
	tracker.RecordToolResult(span, `{"result":"`+strings.Repeat("b", 256)+`"}`, true)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, "execute_tool", mustStringAttr(t, attrs, OperationNameKey))
	assert.Equal(t, "search", mustStringAttr(t, attrs, ToolNameKey))
	assert.Equal(t, "call-1", mustStringAttr(t, attrs, ToolCallIDKey))
	arguments := mustStringAttr(t, attrs, ToolCallArgumentsKey)
	assert.NotEmpty(t, arguments)
	assert.Contains(t, arguments, `{"q":`)
	assert.False(t, json.Valid([]byte(arguments)))
	assert.NotEmpty(t, mustStringAttr(t, attrs, ToolCallResultKey))
	assert.True(t, mustBoolAttr(t, attrs, ToolErrorKey))
}

func TestRecordInteraction_TruncatedPayload_MayBeInvalidJSON_ButUTF8Safe(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("genai-test"),
		tp.Tracer("genai-test"),
		WithRecordPayloads(true),
		WithMaxContextLength(80),
	)
	require.NoError(t, err)

	payload := Payload{
		InputMessages: []Message{{
			Role: "user",
			Parts: []ContentPart{{
				Type:    "text",
				Content: strings.Repeat("你", 512),
			}},
		}},
	}

	_, span := tp.Tracer("genai-test").Start(context.Background(), "span")
	tracker.RecordInteraction(context.Background(), span, testMeta(), payload, Usage{})
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	value := mustStringAttr(t, attrs, InputMessagesKey)
	assert.LessOrEqual(t, len(value), 80)
	assert.True(t, utf8.ValidString(value))
	assert.True(t, strings.HasSuffix(value, truncationSuffix))
	assert.False(t, json.Valid([]byte(value)))
}

func TestRecordCacheHit_AndRecordAgentStep(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := tp.Tracer("genai-test").Start(context.Background(), "span")
	RecordCacheHit(span, true, "pgvector_cache")
	RecordAgentStep(span, "cardiologist", "specialist", "step-2")
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.True(t, mustBoolAttr(t, attrs, CacheHitKey))
	assert.Equal(t, "pgvector_cache", mustStringAttr(t, attrs, RetrievalSourceKey))
	require.Len(t, spans[0].Events, 1)
	assert.Equal(t, AgentStepEvent, spans[0].Events[0].Name)
}

func testMeta() Meta {
	return Meta{
		Provider:      "openai",
		Operation:     "chat",
		RequestModel:  "gpt-4o-mini",
		ResponseModel: "gpt-4o-mini",
		Duration:      2 * time.Second,
	}
}

func testPayload() Payload {
	return Payload{
		SystemInstructions: []ContentPart{{
			Type:    "text",
			Content: "You are concise.",
		}},
		InputMessages: []Message{{
			Role: "user",
			Parts: []ContentPart{{
				Type:    "text",
				Content: "What is 2+2?",
			}},
		}},
		OutputMessages: []Message{{
			Role: "assistant",
			Parts: []ContentPart{{
				Type:    "text",
				Content: "4",
			}},
			FinishReason: "stop",
		}},
	}
}

func mustStringAttr(t *testing.T, attrs attribute.Set, key attribute.Key) string {
	t.Helper()
	value, ok := attrs.Value(key)
	require.True(t, ok)
	return value.AsString()
}

func mustIntAttr(t *testing.T, attrs attribute.Set, key attribute.Key) int64 {
	t.Helper()
	value, ok := attrs.Value(key)
	require.True(t, ok)
	return value.AsInt64()
}

func mustFloatAttr(t *testing.T, attrs attribute.Set, key attribute.Key) float64 {
	t.Helper()
	value, ok := attrs.Value(key)
	require.True(t, ok)
	return value.AsFloat64()
}

func mustBoolAttr(t *testing.T, attrs attribute.Set, key attribute.Key) bool {
	t.Helper()
	value, ok := attrs.Value(key)
	require.True(t, ok)
	return value.AsBool()
}

func newParentSpanContext(remote bool) trace.SpanContext {
	traceID := trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	spanID := trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     remote,
	})
}
