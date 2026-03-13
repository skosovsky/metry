package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRecordInteraction_SetsAttributes(t *testing.T) {
	// Use a simple span recorder to capture attributes without async export
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordInteraction(rec, "What is 2+2?", "4")
	assert.Equal(t, "What is 2+2?", attrs[PromptKey].AsString())
	assert.Equal(t, "4", attrs[CompletionKey].AsString())
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

func TestRecordAgentState_SetsAttributes(t *testing.T) {
	attrs := make(map[attribute.Key]attribute.Value)
	rec := &recordingSpan{attrs: attrs}
	RecordAgentState(rec, "cardiologist", "specialist", "step-2")
	assert.Equal(t, "cardiologist", attrs[AgentNameKey].AsString())
	assert.Equal(t, "specialist", attrs[AgentRoleKey].AsString())
	assert.Equal(t, "step-2", attrs[WorkflowStepKey].AsString())
}

// recordingSpan implements trace.Span and records SetAttributes for tests.
type recordingSpan struct {
	noop.Span
	attrs map[attribute.Key]attribute.Value
}

func (r *recordingSpan) SetAttributes(kv ...attribute.KeyValue) {
	for _, a := range kv {
		r.attrs[a.Key] = a.Value
	}
}
