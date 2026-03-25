package genai

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRecordInteraction_DefaultPrivacy_DoesNotSetPayloadAttributes(t *testing.T) {
	installDefaultTrackerForTest(t, noopTracker)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	RecordInteraction(context.Background(), rec, testMeta(), testPayload(), GenAIUsage{})

	_, ok := rec.attrs[SystemInstructionsKey]
	assert.False(t, ok)
	_, ok = rec.attrs[InputMessagesKey]
	assert.False(t, ok)
	_, ok = rec.attrs[OutputMessagesKey]
	assert.False(t, ok)
	assert.Equal(t, "openai", rec.attrs[ProviderNameKey].AsString())
	assert.Equal(t, "chat", rec.attrs[OperationNameKey].AsString())
}

func TestRecordInteraction_WithPayloads_SetsOfficialPayloadAndUsageAttrs(t *testing.T) {
	tracker, err := NewTracker(nil, WithRecordPayloads(true))
	require.NoError(t, err)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	tracker.RecordInteraction(context.Background(), rec, testMeta(), testPayload(), GenAIUsage{
		InputTokens:           10,
		OutputTokens:          20,
		ReasoningOutputTokens: 4,
		Cost:                  0.25,
		Currency:              "CREDITS",
	})

	assert.JSONEq(t, `[{"type":"text","content":"You are concise."}]`, rec.attrs[SystemInstructionsKey].AsString())
	assert.JSONEq(
		t,
		`[{"role":"user","parts":[{"type":"text","content":"What is 2+2?"}]}]`,
		rec.attrs[InputMessagesKey].AsString(),
	)
	assert.JSONEq(
		t,
		`[{"role":"assistant","parts":[{"type":"text","content":"4"}],"finish_reason":"stop"}]`,
		rec.attrs[OutputMessagesKey].AsString(),
	)
	assert.Equal(t, int64(10), rec.attrs[InputTokensKey].AsInt64())
	assert.Equal(t, int64(20), rec.attrs[OutputTokensKey].AsInt64())
	assert.Equal(t, int64(4), rec.attrs[UsageReasoningOutputTokensKey].AsInt64())
	assert.InDelta(t, 0.25, rec.attrs[UsageCostKey].AsFloat64(), 1e-9)
	assert.Equal(t, "CREDITS", rec.attrs[CostCurrencyKey].AsString())
}

func TestRecordInteraction_CacheOnlyUsage_DoesNotEmitSyntheticZeroAttrs(t *testing.T) {
	tracker, err := NewTracker(nil)
	require.NoError(t, err)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	tracker.RecordInteraction(context.Background(), rec, testMeta(), GenAIPayload{}, GenAIUsage{
		CacheReadInputTokens: 7,
	})

	assert.Equal(t, int64(7), rec.attrs[CacheReadInputTokensKey].AsInt64())
	_, ok := rec.attrs[InputTokensKey]
	assert.False(t, ok)
	_, ok = rec.attrs[OutputTokensKey]
	assert.False(t, ok)
	_, ok = rec.attrs[UsageCostKey]
	assert.False(t, ok)
	_, ok = rec.attrs[CostCurrencyKey]
	assert.False(t, ok)
	_, ok = rec.attrs[OperationPurposeKey]
	assert.False(t, ok)
}

func TestRecordInteraction_VideoOnlyUsage_DoesNotEmitSyntheticZeroAttrs(t *testing.T) {
	tracker, err := NewTracker(nil)
	require.NoError(t, err)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	tracker.RecordInteraction(context.Background(), rec, testMeta(), GenAIPayload{}, GenAIUsage{
		VideoFrames: 24,
	})

	assert.Equal(t, int64(24), rec.attrs[UsageVideoFramesKey].AsInt64())
	_, ok := rec.attrs[InputTokensKey]
	assert.False(t, ok)
	_, ok = rec.attrs[OutputTokensKey]
	assert.False(t, ok)
	_, ok = rec.attrs[UsageCostKey]
	assert.False(t, ok)
	_, ok = rec.attrs[CostCurrencyKey]
	assert.False(t, ok)
	_, ok = rec.attrs[OperationPurposeKey]
	assert.False(t, ok)
}

func TestRecordInteraction_CostOnlyUsage_EmitsDefaultPurposeWithoutSyntheticTokenZeros(t *testing.T) {
	tracker, err := NewTracker(nil)
	require.NoError(t, err)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	tracker.RecordInteraction(context.Background(), rec, testMeta(), GenAIPayload{}, GenAIUsage{
		Cost: 0.25,
	})

	assert.InDelta(t, 0.25, rec.attrs[UsageCostKey].AsFloat64(), 1e-9)
	assert.Equal(t, "USD", rec.attrs[CostCurrencyKey].AsString())
	assert.Equal(t, PurposeGeneration, rec.attrs[OperationPurposeKey].AsString())
	_, ok := rec.attrs[InputTokensKey]
	assert.False(t, ok)
	_, ok = rec.attrs[OutputTokensKey]
	assert.False(t, ok)
}

func TestRecordInteraction_PurposeOnlyUsage_EmitsExplicitPurposeWithoutCostAttrs(t *testing.T) {
	tracker, err := NewTracker(nil)
	require.NoError(t, err)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	tracker.RecordInteraction(context.Background(), rec, testMeta(), GenAIPayload{}, GenAIUsage{
		Purpose: PurposeGuardEvaluation,
	})

	assert.Equal(t, PurposeGuardEvaluation, rec.attrs[OperationPurposeKey].AsString())
	_, ok := rec.attrs[UsageCostKey]
	assert.False(t, ok)
	_, ok = rec.attrs[CostCurrencyKey]
	assert.False(t, ok)
}

func TestRecordInteraction_TruncatedPayloadJSON_RemainsValid(t *testing.T) {
	tracker, err := NewTracker(nil, WithRecordPayloads(true), WithMaxContextLength(96))
	require.NoError(t, err)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	long := strings.Repeat("a", 2048)
	payload := GenAIPayload{
		InputMessages: []GenAIMessage{{
			Role: "user",
			Parts: []GenAIContentPart{{
				Type:    "text",
				Content: long,
			}},
		}},
	}
	tracker.RecordInteraction(context.Background(), rec, testMeta(), payload, GenAIUsage{})

	value := rec.attrs[InputMessagesKey].AsString()
	assert.LessOrEqual(t, len(value), 96)
	assert.True(t, json.Valid([]byte(value)))
}

func TestRecordInteraction_InvalidPayloadToolJSON_DropsMalformedFieldButKeepsPayloadAttr(t *testing.T) {
	tracker, err := NewTracker(nil, WithRecordPayloads(true))
	require.NoError(t, err)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	payload := GenAIPayload{
		InputMessages: []GenAIMessage{{
			Role: "assistant",
			Parts: []GenAIContentPart{{
				Type:      "tool_call",
				Name:      "search",
				Arguments: json.RawMessage(`{"q":`),
			}},
		}},
	}
	tracker.RecordInteraction(context.Background(), rec, testMeta(), payload, GenAIUsage{})

	value := rec.attrs[InputMessagesKey].AsString()
	require.True(t, json.Valid([]byte(value)))
	assert.NotContains(t, value, `"arguments":`)
}

func TestStartToolSpan_SetsOfficialToolAttrs(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTrackerWithTracer(nil, tp.Tracer("tool-test"))
	require.NoError(t, err)

	_, span := tracker.StartToolSpan(context.Background(), "search", "call-1", `{"q":"test"}`)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, "execute_tool", mustStringAttr(t, attrs, OperationNameKey))
	assert.Equal(t, "search", mustStringAttr(t, attrs, ToolNameKey))
	assert.Equal(t, "call-1", mustStringAttr(t, attrs, ToolCallIDKey))
	assert.JSONEq(t, `{"q":"test"}`, mustStringAttr(t, attrs, ToolCallArgumentsKey))
}

func TestStartToolSpan_TruncatedArgsJSON_RemainsValid(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTrackerWithTracer(nil, tp.Tracer("tool-test"), WithMaxContextLength(96))
	require.NoError(t, err)

	_, span := tracker.StartToolSpan(context.Background(), "search", "call-1", `{"q":"`+strings.Repeat("a", 2048)+`"}`)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	value := mustStringAttr(t, attrs, ToolCallArgumentsKey)
	assert.LessOrEqual(t, len(value), 96)
	assert.True(t, json.Valid([]byte(value)))
}

func TestStartToolSpan_InvalidArgsJSON_DropsAttribute(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTrackerWithTracer(nil, tp.Tracer("tool-test"))
	require.NoError(t, err)

	_, span := tracker.StartToolSpan(context.Background(), "search", "call-1", `{"q":`)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	_, ok := attrs.Value(ToolCallArgumentsKey)
	assert.False(t, ok)
}

func TestStartToolSpan_DefaultTrackerIsTracingNoopBeforeInit(t *testing.T) {
	installDefaultTrackerForTest(t, nil)

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	prevTracerProvider := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prevTracerProvider) })
	otel.SetTracerProvider(tp)

	_, span := StartToolSpan(context.Background(), "search", "call-1", `{"q":"test"}`)
	span.End()

	require.Empty(t, mem.GetSpans())
}

func TestRecordToolResult_TruncatedResultJSON_RemainsValid(t *testing.T) {
	tracker, err := NewTracker(nil, WithMaxContextLength(96))
	require.NoError(t, err)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	tracker.RecordToolResult(rec, `{"result":"`+strings.Repeat("b", 2048)+`"}`, false)

	value := rec.attrs[ToolCallResultKey].AsString()
	assert.LessOrEqual(t, len(value), 96)
	assert.True(t, json.Valid([]byte(value)))
	assert.False(t, rec.attrs[ToolErrorKey].AsBool())
	assert.Equal(t, codes.Ok, rec.statusCode)
}

func TestRecordToolResult_InvalidResultJSON_DropsAttributeAndSetsErrorStatus(t *testing.T) {
	tracker, err := NewTracker(nil)
	require.NoError(t, err)

	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	tracker.RecordToolResult(rec, `{"result":`, true)

	_, ok := rec.attrs[ToolCallResultKey]
	assert.False(t, ok)
	assert.True(t, rec.attrs[ToolErrorKey].AsBool())
	assert.Equal(t, codes.Error, rec.statusCode)
}

func TestStartToolSpan_ConcurrentToolCalls(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTrackerWithTracer(nil, tp.Tracer("tool-test"))
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, span := tracker.StartToolSpan(context.Background(), "tool", "id-"+strings.Repeat("x", id+1), `{}`)
			tracker.RecordToolResult(span, `{"ok":true}`, false)
			span.End()
		}(i)
	}
	wg.Wait()

	require.Len(t, mem.GetSpans(), 10)
}

func TestRecordCacheHit_SetsAttributes(t *testing.T) {
	rec := &recordingSpan{attrs: make(map[attribute.Key]attribute.Value)}
	RecordCacheHit(rec, true, "pgvector_cache")
	assert.True(t, rec.attrs[CacheHitKey].AsBool())
	assert.Equal(t, "pgvector_cache", rec.attrs[RetrievalSourceKey].AsString())
}

func TestRecordAgentStep_AddsEvent(t *testing.T) {
	rec := &recordingSpan{events: make([]recordedEvent, 0)}
	RecordAgentStep(rec, "cardiologist", "specialist", "step-2")
	require.Len(t, rec.events, 1)
	assert.Equal(t, AgentStepEvent, rec.events[0].name)
}

func TestRecordAgentStep_EventAttributes_RealSpan(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	prevTracerProvider := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prevTracerProvider) })
	otel.SetTracerProvider(tp)

	_, span := otel.Tracer("genai-test").Start(context.Background(), "op")
	RecordAgentStep(span, "cardiologist", "specialist", "step-2")
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)
	evt := spans[0].Events[0]
	assert.Equal(t, AgentStepEvent, evt.Name)
	attrs := attribute.NewSet(evt.Attributes...)
	assert.Equal(t, "cardiologist", mustStringAttr(t, attrs, AgentNameKey))
	assert.Equal(t, "specialist", mustStringAttr(t, attrs, AgentRoleKey))
	assert.Equal(t, "step-2", mustStringAttr(t, attrs, WorkflowStepKey))
}

func testMeta() GenAIMeta {
	return GenAIMeta{
		Provider:      "openai",
		Operation:     "chat",
		RequestModel:  "gpt-4o-mini",
		ResponseModel: "gpt-4o-mini",
		Duration:      2 * time.Second,
	}
}

func testPayload() GenAIPayload {
	return GenAIPayload{
		SystemInstructions: []GenAIContentPart{{
			Type:    "text",
			Content: "You are concise.",
		}},
		InputMessages: []GenAIMessage{{
			Role: "user",
			Parts: []GenAIContentPart{{
				Type:    "text",
				Content: "What is 2+2?",
			}},
		}},
		OutputMessages: []GenAIMessage{{
			Role: "assistant",
			Parts: []GenAIContentPart{{
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

type recordedEvent struct {
	name  string
	attrs map[attribute.Key]attribute.Value
}

type recordingSpan struct {
	noop.Span

	attrs      map[attribute.Key]attribute.Value
	events     []recordedEvent
	statusCode codes.Code
	statusDesc string
}

func (r *recordingSpan) SetAttributes(kv ...attribute.KeyValue) {
	if r.attrs == nil {
		r.attrs = make(map[attribute.Key]attribute.Value)
	}
	for _, a := range kv {
		r.attrs[a.Key] = a.Value
	}
}

func (r *recordingSpan) SetStatus(code codes.Code, description string) {
	r.statusCode = code
	r.statusDesc = description
}

func (r *recordingSpan) AddEvent(name string, options ...trace.EventOption) {
	cfg := trace.NewEventConfig(options...)
	attrs := make(map[attribute.Key]attribute.Value, len(cfg.Attributes()))
	for _, a := range cfg.Attributes() {
		attrs[a.Key] = a.Value
	}
	r.events = append(r.events, recordedEvent{name: name, attrs: attrs})
}
