package genai

import (
	"context"
	"unicode/utf8"

	"github.com/skosovsky/metry/internal/genaimetrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// maxContextLength is the maximum byte length for prompt, completion, and tool result
// strings before truncation. Not exported so callers cannot weaken OOM protection.
const maxContextLength = 16384

const truncationSuffix = "... [TRUNCATED]"

// truncateContext returns s if len(s) <= maxContextLength; otherwise returns a UTF-8-safe
// prefix of s (plus truncationSuffix) so the result does not cut a rune in half.
func truncateContext(s string) string {
	if maxContextLength <= 0 {
		return ""
	}
	if len(s) <= maxContextLength {
		return s
	}
	maxContent := maxContextLength - len(truncationSuffix)
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
	holder := genaimetrics.Holder()
	if holder != nil {
		if holder.InputTokens != nil {
			holder.InputTokens.Add(ctx, int64(inTokens), opts)
		}
		if holder.OutputTokens != nil {
			holder.OutputTokens.Add(ctx, int64(outTokens), opts)
		}
		if holder.Cost != nil {
			holder.Cost.Add(ctx, costUSD, opts)
		}
	}
}

// RecordInteraction sets prompt and completion attributes on the span (OpenLLMetry conventions).
// Long strings are truncated to maxContextLength to protect export pipelines.
func RecordInteraction(span trace.Span, prompt, completion string) {
	span.SetAttributes(
		attribute.String(Prompt, truncateContext(prompt)),
		attribute.String(Completion, truncateContext(completion)),
	)
}

// RecordToolCall sets tool call attributes on the span. Call from toolsy before executing a tool.
func RecordToolCall(span trace.Span, name, id, argsJSON string) {
	span.SetAttributes(
		attribute.String(ToolName, name),
		attribute.String(ToolID, id),
		attribute.String(ToolArgs, truncateContext(argsJSON)),
	)
}

// RecordToolResult records the result of a tool call (e.g. for agent loops).
// resultJSON is truncated to maxContextLength; isError marks a failed tool invocation.
// Event name gen_ai.tool.result follows OTel GenAI semantic conventions for dashboards.
func RecordToolResult(span trace.Span, toolName string, resultJSON string, isError bool) {
	span.AddEvent("gen_ai.tool.result", trace.WithAttributes(
		ToolNameKey.String(toolName),
		ToolResultKey.String(truncateContext(resultJSON)),
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
// Event name gen_ai.agent.step and attributes follow OTel GenAI semantic conventions for dashboards.
// Call from flowy on each state transition; multiple calls on the same span produce a chronological event list.
func RecordAgentStep(span trace.Span, agentName, agentRole, step string) {
	span.AddEvent("gen_ai.agent.step", trace.WithAttributes(
		AgentNameKey.String(agentName),
		AgentRoleKey.String(agentRole),
		WorkflowStepKey.String(step),
	))
}
