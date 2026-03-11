package genai

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RecordUsage sets token usage and cost attributes on the span and increments
// OTel counters if genai.Init was called (OpenLLMetry conventions).
// Use for LLM request/response usage so backends (e.g. Langfuse, Phoenix) can show cost and token counts.
func RecordUsage(ctx context.Context, span trace.Span, inTokens, outTokens int, costUSD float64) {
	span.SetAttributes(
		attribute.Int(InputTokens, inTokens),
		attribute.Int(OutputTokens, outTokens),
		attribute.Float64(CostUSD, costUSD),
	)
	if inputTokensCounter != nil {
		inputTokensCounter.Add(ctx, int64(inTokens))
	}
	if outputTokensCounter != nil {
		outputTokensCounter.Add(ctx, int64(outTokens))
	}
	if costCounter != nil {
		costCounter.Add(ctx, costUSD)
	}
}

// RecordInteraction sets prompt and completion attributes on the span (OpenLLMetry conventions).
// Use for recording the user prompt and model completion on the current span.
func RecordInteraction(span trace.Span, prompt, completion string) {
	span.SetAttributes(
		attribute.String(Prompt, prompt),
		attribute.String(Completion, completion),
	)
}

// RecordToolCall sets tool call attributes on the span. Call from toolsy before executing a tool.
func RecordToolCall(span trace.Span, name, id, argsJSON string) {
	span.SetAttributes(
		attribute.String(ToolName, name),
		attribute.String(ToolID, id),
		attribute.String(ToolArgs, argsJSON),
	)
}

// RecordCacheHit records cache hit and retrieval source on the span. Call from RAG layer before LLM request.
func RecordCacheHit(span trace.Span, hit bool, source string) {
	span.SetAttributes(
		attribute.Bool(CacheHit, hit),
		attribute.String(RetrievalSource, source),
	)
}

// RecordAgentState sets agent and workflow step attributes on the span. Call from flowy on state machine transition.
func RecordAgentState(span trace.Span, agentName, agentRole, step string) {
	span.SetAttributes(
		attribute.String(AgentName, agentName),
		attribute.String(AgentRole, agentRole),
		attribute.String(WorkflowStep, step),
	)
}
