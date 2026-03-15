package genai

import (
	"context"
	"sync/atomic"
	"unicode/utf8"

	"github.com/skosovsky/metry/internal/genaimetrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// tracerName ties tool spans to the main provider set by metry.Init.
const tracerName = "metry"

func getTracer() trace.Tracer { return otel.Tracer(tracerName) }

var maxContextLength atomic.Int32

const truncationSuffix = "... [TRUNCATED]"

func init() {
	maxContextLength.Store(16384)
}

// SetMaxContextLength is an internal API. DO NOT use directly; configure via metry.WithMaxGenAIContextLength.
func SetMaxContextLength(limit int32) {
	maxContextLength.Store(limit)
}

// truncateContext returns s if len(s) <= limit; otherwise returns a UTF-8-safe
// prefix of s so that len(out) <= limit. If limit allows, appends truncationSuffix; otherwise returns prefix only.
func truncateContext(s string) string {
	limit := int(maxContextLength.Load())
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	// Edge case: limit is smaller than suffix — return UTF-8-safe prefix only (no suffix).
	if limit <= len(truncationSuffix) {
		for limit > 0 && !utf8.ValidString(s[:limit]) {
			limit--
		}
		return s[:limit]
	}
	cutLimit := limit - len(truncationSuffix)
	for cutLimit > 0 && !utf8.ValidString(s[:cutLimit]) {
		cutLimit--
	}
	trunc := s[:cutLimit]
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

// StartToolSpan creates a child span for a tool invocation. Caller MUST call span.End() (e.g. via defer).
// Use for parallel tool calls so each has its own span and timing.
func StartToolSpan(ctx context.Context, toolName, toolID, argsJSON string) (context.Context, trace.Span) {
	ctx, span := getTracer().Start(ctx, "tool: "+toolName)
	span.SetAttributes(
		ToolNameKey.String(toolName),
		ToolIDKey.String(toolID),
		ToolArgsKey.String(truncateContext(argsJSON)),
	)
	return ctx, span
}

// RecordToolResult records the result of a tool call on its own span (from StartToolSpan).
// resultJSON is truncated; isError sets span status to Error for dashboard visibility.
func RecordToolResult(span trace.Span, resultJSON string, isError bool) {
	span.SetAttributes(ToolResultKey.String(truncateContext(resultJSON)))
	if isError {
		span.SetStatus(codes.Error, "tool execution failed")
	} else {
		span.SetStatus(codes.Ok, "")
	}
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
