package genai

import (
	"context"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// MaxContextLength is the maximum byte length for prompt, completion, and tool result
// strings before truncation. Truncation protects OTLP export from oversized payloads (e.g. 4MB gRPC limit).
const MaxContextLength = 16384

const truncationSuffix = "... [TRUNCATED]"

// truncateContext returns s if len(s) <= maxLen; otherwise returns a UTF-8-safe
// prefix of s (plus truncationSuffix) so the result does not cut a rune in half.
func truncateContext(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	maxContent := maxLen - len(truncationSuffix)
	if maxContent <= 0 {
		return truncationSuffix
	}
	trunc := s[:maxContent]
	for len(trunc) > 0 && !utf8.ValidString(trunc) {
		trunc = trunc[:len(trunc)-1]
	}
	return trunc + truncationSuffix
}

// RecordUsage sets token usage and cost attributes on the span and increments
// OTel counters when metry.Init was called with a metric exporter, with purpose defaulting to PurposeGeneration.
func RecordUsage(ctx context.Context, span trace.Span, inTokens, outTokens int, costUSD float64) {
	RecordUsageWithPurpose(ctx, span, inTokens, outTokens, costUSD, PurposeGeneration)
}

// RecordUsageWithPurpose records usage with an explicit purpose so metrics can be
// split by generation vs guard_evaluation vs quality_evaluation (billing, dashboards).
func RecordUsageWithPurpose(ctx context.Context, span trace.Span, inTokens, outTokens int, costUSD float64, purpose string) {
	if purpose == "" {
		purpose = PurposeGeneration
	}
	span.SetAttributes(
		attribute.Int(InputTokens, inTokens),
		attribute.Int(OutputTokens, outTokens),
		attribute.Float64(CostUSD, costUSD),
		OperationPurposeKey.String(purpose),
	)
	opts := metric.WithAttributes(OperationPurposeKey.String(purpose))
	holder := globalMetrics.Load()
	if holder != nil {
		if holder.inputTokens != nil {
			holder.inputTokens.Add(ctx, int64(inTokens), opts)
		}
		if holder.outputTokens != nil {
			holder.outputTokens.Add(ctx, int64(outTokens), opts)
		}
		if holder.cost != nil {
			holder.cost.Add(ctx, costUSD, opts)
		}
	}
}

// RecordInteraction sets prompt and completion attributes on the span (OpenLLMetry conventions).
// Long strings are truncated to MaxContextLength to protect export pipelines.
func RecordInteraction(span trace.Span, prompt, completion string) {
	span.SetAttributes(
		attribute.String(Prompt, truncateContext(prompt, MaxContextLength)),
		attribute.String(Completion, truncateContext(completion, MaxContextLength)),
	)
}

// RecordToolCall sets tool call attributes on the span. Call from toolsy before executing a tool.
func RecordToolCall(span trace.Span, name, id, argsJSON string) {
	span.SetAttributes(
		attribute.String(ToolName, name),
		attribute.String(ToolID, id),
		attribute.String(ToolArgs, truncateContext(argsJSON, MaxContextLength)),
	)
}

// RecordToolResult records the result of a tool call (e.g. for agent loops).
// result is truncated to MaxContextLength; isError marks a failed tool invocation.
func RecordToolResult(span trace.Span, toolName string, result string, isError bool) {
	span.AddEvent("tool_result", trace.WithAttributes(
		ToolNameKey.String(toolName),
		ToolResultKey.String(truncateContext(result, MaxContextLength)),
		ToolErrorKey.Bool(isError),
	))
}

// RecordCacheHit records cache hit and retrieval source on the span. Call from RAG layer before LLM request.
func RecordCacheHit(span trace.Span, hit bool, source string) {
	span.SetAttributes(
		attribute.Bool(CacheHit, hit),
		attribute.String(RetrievalSource, source),
	)
}

// RecordAgentStep records one agent step as a span event (ReAct loops: Thought -> Action -> Observation).
// Step name is set as gen_ai.workflow.step per OTel GenAI semantic conventions for dashboard compatibility.
// Call from flowy on each state transition; multiple calls on the same span produce a chronological event list.
func RecordAgentStep(span trace.Span, agentName, agentRole, step string) {
	span.AddEvent("agent_step", trace.WithAttributes(
		AgentNameKey.String(agentName),
		AgentRoleKey.String(agentRole),
		WorkflowStepKey.String(step),
	))
}
