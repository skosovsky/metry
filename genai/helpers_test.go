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

func TestRecordInteraction_SetsAttributes(t *testing.T) {
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordInteraction(rec, "What is 2+2?", "4")
	assert.Equal(t, "What is 2+2?", attrs[PromptKey].AsString())
	assert.Equal(t, "4", attrs[CompletionKey].AsString())
}

const testContextLimit = 16384 // must match genai.maxContextLength for truncation tests

func TestRecordInteraction_TruncatesLongStrings(t *testing.T) {
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	long := strings.Repeat("a", testContextLimit+100)
	RecordInteraction(rec, long, "short")
	prompt := attrs[PromptKey].AsString()
	assert.LessOrEqual(t, len(prompt), testContextLimit)
	assert.True(t, strings.HasSuffix(prompt, truncationSuffix), "prompt should end with truncation suffix")
	assert.Equal(t, "short", attrs[CompletionKey].AsString())
}

// TestRecordInteraction_OneMegabytePrompt_TruncatesWithoutPanic proves DoD: a 1MB prompt does not
// cause panic or OOM; RecordInteraction truncates to maxContextLength and appends truncation suffix.
func TestRecordInteraction_OneMegabytePrompt_TruncatesWithoutPanic(t *testing.T) {
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	prompt1MB := strings.Repeat("a", 1_000_000)
	completion := "ok"
	RecordInteraction(rec, prompt1MB, completion)
	prompt := attrs[PromptKey].AsString()
	assert.LessOrEqual(t, len(prompt), testContextLimit, "prompt must be truncated to maxContextLength")
	assert.True(t, strings.HasSuffix(prompt, truncationSuffix), "truncated prompt must end with ... [TRUNCATED]")
	assert.Equal(t, completion, attrs[CompletionKey].AsString())
	require.True(t, utf8.ValidString(prompt), "truncated output must remain valid UTF-8 for export")
}

func TestTruncateContext_UTF8Safe(t *testing.T) {
	// Build a string longer than maxContextLength with a multi-byte rune near the cut point
	// so that truncation does not cut the rune in half (UTF-8 safe).
	// a + e-acute + b = 1+2+1 = 4 bytes; repeat to exceed limit then add rune near boundary.
	base := "a\u00e9b"
	s := strings.Repeat(base, 5000) // 20000 bytes > 16384
	out := truncateContext(s)
	assert.True(t, strings.HasSuffix(out, truncationSuffix))
	assert.LessOrEqual(t, len(out), testContextLimit, "truncated result including suffix must be <= maxContextLength (16384) for transport limits")
	require.True(t, utf8.ValidString(out), "truncated string must be valid UTF-8 for export")
}

func TestRecordUsage_Unit(t *testing.T) {
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordUsage(context.Background(), rec, 10, 20, 0.001)
	assert.Equal(t, int64(10), attrs[InputTokensKey].AsInt64())
	assert.Equal(t, int64(20), attrs[OutputTokensKey].AsInt64())
	assert.InDelta(t, 0.001, attrs[CostUSDKey].AsFloat64(), 1e-9)
	assert.Equal(t, PurposeGeneration, attrs[OperationPurposeKey].AsString(), "RecordUsage defaults to PurposeGeneration")
}

func TestRecordUsageWithPurpose_SetsPurposeOnSpan(t *testing.T) {
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordUsageWithPurpose(context.Background(), rec, 5, 10, 0.002, PurposeGuardEvaluation)
	assert.Equal(t, PurposeGuardEvaluation, attrs[OperationPurposeKey].AsString())
	assert.Equal(t, int64(5), attrs[InputTokensKey].AsInt64())
	assert.Equal(t, int64(10), attrs[OutputTokensKey].AsInt64())
}

func TestRecordUsageWithPurpose_EmptyPurpose_WritesGenerationOnSpan(t *testing.T) {
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordUsageWithPurpose(context.Background(), rec, 1, 2, 0.001, "")
	assert.Equal(t, PurposeGeneration, attrs[OperationPurposeKey].AsString(), "empty purpose should normalize to PurposeGeneration")
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
	// Verify exported span has Error status set by RecordToolResult(span, _, true).
	assert.Equal(t, codes.Error, s.Status.Code, "tool span with isError=true must export status Error")
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
	require.Len(t, spans, 10, "concurrent StartToolSpan -> RecordToolResult -> End must produce 10 independent spans")
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

func TestTruncateContext_ConfigurableLimit(t *testing.T) {
	SetMaxContextLength(100)
	defer SetMaxContextLength(16384)
	long := strings.Repeat("a", 500)
	out := truncateContext(long)
	assert.LessOrEqual(t, len(out), 100)
	assert.True(t, strings.HasSuffix(out, truncationSuffix))
	require.True(t, utf8.ValidString(out))
}

func TestTruncateContext_SmallLimit_NoSuffix(t *testing.T) {
	// When limit <= len(truncationSuffix), result is prefix only (no suffix) so len(out) <= limit.
	SetMaxContextLength(5)
	defer SetMaxContextLength(16384)
	out := truncateContext("hello world")
	assert.LessOrEqual(t, len(out), 5)
	assert.NotContains(t, out, truncationSuffix, "small limit must not add suffix")
	require.True(t, utf8.ValidString(out))
}

func TestTruncateContext_SmallLimit10_UTF8Safe(t *testing.T) {
	SetMaxContextLength(10)
	defer SetMaxContextLength(16384)
	// Multi-byte rune at index 9: 8 ASCII + 2-byte rune = 10 bytes
	s := "12345678\u00e9"
	out := truncateContext(s + " more")
	assert.LessOrEqual(t, len(out), 10)
	require.True(t, utf8.ValidString(out))
	assert.NotContains(t, out, truncationSuffix)
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

func (r *recordingSpan) SetStatus(code codes.Code, desc string) {
	r.statusCode = code
	r.statusDesc = desc
}

func (r *recordingSpan) AddEvent(name string, _ ...trace.EventOption) {
	if r.events == nil {
		r.events = make([]recordedEvent, 0)
	}
	// EventOption from otel/trace cannot be unwrapped here; record name only for unit tests.
	// Use TestRecordAgentStep_EventAttributes_RealSpan for full attribute checks.
	r.events = append(r.events, recordedEvent{name: name, attrs: nil})
}
