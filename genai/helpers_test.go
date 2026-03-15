package genai

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

func TestRecordToolCall_SetsAttributes(t *testing.T) {
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordToolCall(rec, "search", "call-1", `{"q":"test"}`)
	assert.Equal(t, "search", attrs[ToolNameKey].AsString())
	assert.Equal(t, "call-1", attrs[ToolIDKey].AsString())
	assert.JSONEq(t, `{"q":"test"}`, attrs[ToolArgsKey].AsString())
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

func TestRecordToolResult_AddsEvent(t *testing.T) {
	rec := &recordingSpan{events: make([]recordedEvent, 0)}
	RecordToolResult(rec, "search", `{"results":["a","b"]}`, false)
	require.Len(t, rec.events, 1)
	assert.Equal(t, "gen_ai.tool.result", rec.events[0].name)
}

func TestRecordToolResult_EventAttributesAndTruncation_RealSpan(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)
	_, span := otel.Tracer("genai-test").Start(context.Background(), "op")
	longResult := strings.Repeat("x", testContextLimit+50)
	RecordToolResult(span, "big", longResult, true)
	span.End()
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)
	evt := spans[0].Events[0]
	assert.Equal(t, "gen_ai.tool.result", evt.Name)
	attrs := attribute.NewSet(evt.Attributes...)
	toolNameVal, ok := attrs.Value(ToolNameKey)
	require.True(t, ok)
	assert.Equal(t, "big", toolNameVal.AsString(), "event must include tool name for dashboards")
	resultVal, ok := attrs.Value(ToolResultKey)
	require.True(t, ok)
	result := resultVal.AsString()
	assert.True(t, strings.HasSuffix(result, truncationSuffix))
	assert.LessOrEqual(t, len(result), testContextLimit)
	require.True(t, utf8.ValidString(result), "truncated result must remain valid UTF-8")
	errVal, ok := attrs.Value(ToolErrorKey)
	require.True(t, ok)
	assert.True(t, errVal.AsBool())
}

type recordedEvent struct {
	name  string
	attrs map[attribute.Key]attribute.Value
}

// recordingSpan implements trace.Span and records SetAttributes and AddEvent for tests.
type recordingSpan struct {
	noop.Span
	attrs  map[attribute.Key]attribute.Value
	events []recordedEvent
}

func (r *recordingSpan) SetAttributes(kv ...attribute.KeyValue) {
	if r.attrs == nil {
		r.attrs = make(map[attribute.Key]attribute.Value)
	}
	for _, a := range kv {
		r.attrs[a.Key] = a.Value
	}
}

func (r *recordingSpan) AddEvent(name string, _ ...trace.EventOption) {
	if r.events == nil {
		r.events = make([]recordedEvent, 0)
	}
	// EventOption from otel/trace cannot be unwrapped here; record name only for unit tests.
	// Use TestRecordAgentStep_EventAttributes_RealSpan for full attribute checks.
	r.events = append(r.events, recordedEvent{name: name, attrs: nil})
}
