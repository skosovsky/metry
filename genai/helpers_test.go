package genai

import (
	"context"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

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

const testContextLimit = 16384

func TestRecordInteraction_DefaultPrivacy_DoesNotSetPayloadAttributes(t *testing.T) {
	t.Cleanup(resetRuntimeConfigForTest())

	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordInteraction(context.Background(), rec, GenAIPayload{
		System:     "sys",
		Prompt:     "What is 2+2?",
		Completion: "4",
	}, GenAIUsage{})

	assert.Empty(t, attrs)
}

func TestRecordInteraction_WithRecordPayloads_SetsAttributes(t *testing.T) {
	t.Cleanup(setRuntimeConfigForTest(defaultMaxContextLength, true))

	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordInteraction(context.Background(), rec, GenAIPayload{
		System:     "system prompt",
		Prompt:     "What is 2+2?",
		Completion: "4",
	}, GenAIUsage{})

	assert.Equal(t, "system prompt", attrs[SystemKey].AsString())
	assert.Equal(t, "What is 2+2?", attrs[PromptKey].AsString())
	assert.Equal(t, "4", attrs[CompletionKey].AsString())
}

func TestRecordInteraction_TruncatesLongStrings(t *testing.T) {
	t.Cleanup(setRuntimeConfigForTest(testContextLimit, true))

	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	long := strings.Repeat("a", testContextLimit+100)
	RecordInteraction(context.Background(), rec, GenAIPayload{
		Prompt:     long,
		Completion: "short",
	}, GenAIUsage{})

	prompt := attrs[PromptKey].AsString()
	assert.LessOrEqual(t, len(prompt), testContextLimit)
	assert.True(t, strings.HasSuffix(prompt, truncationSuffix), "prompt should end with truncation suffix")
	assert.Equal(t, "short", attrs[CompletionKey].AsString())
}

func TestRecordInteraction_OneMegabytePrompt_TruncatesWithoutPanic(t *testing.T) {
	t.Cleanup(setRuntimeConfigForTest(testContextLimit, true))

	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	prompt1MB := strings.Repeat("a", 1_000_000)
	completion := "ok"

	RecordInteraction(context.Background(), rec, GenAIPayload{
		Prompt:     prompt1MB,
		Completion: completion,
	}, GenAIUsage{})

	prompt := attrs[PromptKey].AsString()
	assert.LessOrEqual(t, len(prompt), testContextLimit, "prompt must be truncated to maxContextLength")
	assert.True(t, strings.HasSuffix(prompt, truncationSuffix), "truncated prompt must end with ... [TRUNCATED]")
	assert.Equal(t, completion, attrs[CompletionKey].AsString())
	require.True(t, utf8.ValidString(prompt), "truncated output must remain valid UTF-8 for export")
}

func TestTruncateContext_UTF8Safe(t *testing.T) {
	t.Cleanup(setRuntimeConfigForTest(testContextLimit, false))

	base := "a\u00e9b"
	s := strings.Repeat(base, 5000)
	out := truncateContext(s)
	assert.True(t, strings.HasSuffix(out, truncationSuffix))
	assert.LessOrEqual(t, len(out), testContextLimit)
	require.True(t, utf8.ValidString(out))
}

func TestRecordInteraction_WritesUsageAndNormalizesPurpose(t *testing.T) {
	t.Cleanup(resetRuntimeConfigForTest())

	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordInteraction(context.Background(), rec, GenAIPayload{}, GenAIUsage{
		InputTokens:  10,
		OutputTokens: 20,
		CostUSD:      0.001,
	})

	assert.Equal(t, int64(10), attrs[InputTokensKey].AsInt64())
	assert.Equal(t, int64(20), attrs[OutputTokensKey].AsInt64())
	assert.InDelta(t, 0.001, attrs[CostUSDKey].AsFloat64(), 1e-9)
	assert.Equal(t, PurposeGeneration, attrs[OperationPurposeKey].AsString())
}

func TestRecordInteraction_WritesOptionalMultimodalUsage(t *testing.T) {
	t.Cleanup(resetRuntimeConfigForTest())

	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordInteraction(context.Background(), rec, GenAIPayload{}, GenAIUsage{
		AudioSeconds: 12.5,
		ImageCount:   3,
	})

	assert.InDelta(t, 12.5, attrs[AudioSecondsKey].AsFloat64(), 1e-9)
	assert.Equal(t, int64(3), attrs[ImageCountKey].AsInt64())
	assert.Equal(t, PurposeGeneration, attrs[OperationPurposeKey].AsString())
}

func TestTruncateContext_ConfigurableLimit(t *testing.T) {
	t.Cleanup(setRuntimeConfigForTest(100, false))

	long := strings.Repeat("a", 500)
	out := truncateContext(long)
	assert.LessOrEqual(t, len(out), 100)
	assert.True(t, strings.HasSuffix(out, truncationSuffix))
	require.True(t, utf8.ValidString(out))
}

func TestTruncateContext_SmallLimit_NoSuffix(t *testing.T) {
	t.Cleanup(setRuntimeConfigForTest(5, false))

	out := truncateContext("hello world")
	assert.LessOrEqual(t, len(out), 5)
	assert.NotContains(t, out, truncationSuffix)
	require.True(t, utf8.ValidString(out))
}

func TestTruncateContext_SmallLimit10_UTF8Safe(t *testing.T) {
	t.Cleanup(setRuntimeConfigForTest(10, false))

	s := "12345678\u00e9"
	out := truncateContext(s + " more")
	assert.LessOrEqual(t, len(out), 10)
	require.True(t, utf8.ValidString(out))
	assert.NotContains(t, out, truncationSuffix)
}

func TestTruncateContext_InvalidUTF8_O1(t *testing.T) {
	t.Cleanup(setRuntimeConfigForTest(32, false))

	s := string([]byte{0xff}) + strings.Repeat("a", 128)
	out := truncateContext(s)

	assert.LessOrEqual(t, len(out), 32)
	assert.True(t, strings.HasSuffix(out, truncationSuffix))
	assert.NotEqual(t, truncationSuffix, out, "valid suffix of the payload must survive invalid leading bytes")
	assert.True(t, strings.HasPrefix(out, strings.Repeat("a", 4)))
	require.True(t, utf8.ValidString(out))
}

func TestTruncateContext_InvalidUTF8WithinLimit_IsSanitized(t *testing.T) {
	t.Cleanup(setRuntimeConfigForTest(32, false))

	out := truncateContext("ok" + string([]byte{0xff}) + "tail")

	assert.Equal(t, "oktail", out)
	require.True(t, utf8.ValidString(out))
}

func BenchmarkTruncateContext_InvalidUTF8(b *testing.B) {
	restore := setRuntimeConfigForTest(testContextLimit, false)
	defer restore()

	s := string([]byte{0xff}) + strings.Repeat("a", 1_000_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = truncateContext(s)
	}
}

func TestStartToolSpan_SetsAttributesAndReturnsChildSpan(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	_, span := StartToolSpan(context.Background(), "search", "call-1", `{"q":"test"}`)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "tool: search", s.Name)
	attrs := attribute.NewSet(s.Attributes...)
	toolName, _ := attrs.Value(ToolNameKey)
	assert.Equal(t, "search", toolName.AsString())
	toolID, _ := attrs.Value(ToolIDKey)
	assert.Equal(t, "call-1", toolID.AsString())
	toolArgs, _ := attrs.Value(ToolArgsKey)
	assert.JSONEq(t, `{"q":"test"}`, toolArgs.AsString())
}

func TestRecordToolResult_SetsAttributeAndStatus(t *testing.T) {
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordToolResult(rec, `{"ok":true}`, false)
	assert.Equal(t, `{"ok":true}`, attrs[ToolResultKey].AsString())
	assert.Equal(t, codes.Ok, rec.statusCode)

	rec.statusCode = 0
	RecordToolResult(rec, `{"error":"fail"}`, true)
	assert.Equal(t, "tool execution failed", rec.statusDesc)
	assert.Equal(t, codes.Error, rec.statusCode)
}

func TestRecordToolResult_OnToolSpan_AttributesAndStatus(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	_, span := StartToolSpan(context.Background(), "big", "id-1", "{}")
	longResult := strings.Repeat("x", testContextLimit+50)
	RecordToolResult(span, longResult, true)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	s := spans[0]
	attrs := attribute.NewSet(s.Attributes...)
	resultVal, ok := attrs.Value(ToolResultKey)
	require.True(t, ok)
	result := resultVal.AsString()
	assert.True(t, strings.HasSuffix(result, truncationSuffix))
	assert.LessOrEqual(t, len(result), testContextLimit)
	require.True(t, utf8.ValidString(result))
	assert.Equal(t, codes.Error, s.Status.Code)
}

func TestStartToolSpan_ConcurrentToolCalls(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx := context.Background()
			_, span := StartToolSpan(ctx, "tool", "id-"+strings.Repeat("x", id+1), "{}")
			RecordToolResult(span, "ok", false)
			span.End()
		}(i)
	}
	wg.Wait()

	spans := mem.GetSpans()
	require.Len(t, spans, 10)
}

func TestRecordCacheHit_SetsAttributes(t *testing.T) {
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordCacheHit(rec, true, "pgvector_cache")
	assert.True(t, attrs[CacheHitKey].AsBool())
	assert.Equal(t, "pgvector_cache", attrs[RetrievalSourceKey].AsString())
}

func TestRecordAgentStep_AddsEvent(t *testing.T) {
	rec := &recordingSpan{events: make([]recordedEvent, 0)}
	RecordAgentStep(rec, "cardiologist", "specialist", "step-2")
	require.Len(t, rec.events, 1)
	assert.Equal(t, "gen_ai.agent.step", rec.events[0].name)
}

func TestRecordAgentStep_EventAttributes_RealSpan(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	_, span := otel.Tracer("genai-test").Start(context.Background(), "op")
	RecordAgentStep(span, "cardiologist", "specialist", "step-2")
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)
	evt := spans[0].Events[0]
	assert.Equal(t, "gen_ai.agent.step", evt.Name)
	attrs := attribute.NewSet(evt.Attributes...)
	nameVal, ok := attrs.Value(AgentNameKey)
	require.True(t, ok)
	assert.Equal(t, "cardiologist", nameVal.AsString())
	roleVal, ok := attrs.Value(AgentRoleKey)
	require.True(t, ok)
	assert.Equal(t, "specialist", roleVal.AsString())
	stepVal, ok := attrs.Value(WorkflowStepKey)
	require.True(t, ok)
	assert.Equal(t, "step-2", stepVal.AsString())
}

type recordedEvent struct {
	name  string
	attrs map[attribute.Key]attribute.Value
}

// recordingSpan implements trace.Span and records SetAttributes, SetStatus, and AddEvent for tests.
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
