package genai

import (
	"context"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/genaiconfig"
)

// tracerName is the instrumentation scope for genai tool spans (granularity per task6).
const tracerName = "metry/genai"

func getTracer() trace.Tracer { return otel.Tracer(tracerName) }

const (
	truncationSuffix    = "... [TRUNCATED]"
	defaultCostCurrency = "USD"
)

//revive:disable:exported

// GenAIPayload captures the text payloads attached to an interaction span.
type GenAIPayload struct {
	System     string
	Prompt     string
	Completion string
}

// GenAIUsage captures token/cost and multimodal usage recorded on an interaction span.
type GenAIUsage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Currency     string
	AudioSeconds float64
	ImageCount   int
	Purpose      string
}

//revive:enable:exported

// truncateContext returns s if len(s) <= limit; otherwise returns a UTF-8-safe
// prefix of s so that len(out) <= limit. If limit allows, appends truncationSuffix; otherwise returns prefix only.
func truncateContext(s string) string {
	return truncateContextWithConfig(s, currentConfig())
}

func truncateContextWithConfig(s string, cfg *genaiconfig.RuntimeConfig) string {
	limit := cfg.MaxContextLength()
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return strings.ToValidUTF8(s, "")
	}
	// Edge case: limit is smaller than suffix — return UTF-8-safe prefix only (no suffix).
	if limit <= len(truncationSuffix) {
		return strings.ToValidUTF8(truncateAtRuneBoundary(s, limit), "")
	}
	trunc := strings.ToValidUTF8(truncateAtRuneBoundary(s, limit-len(truncationSuffix)), "")
	return trunc + truncationSuffix
}

func truncateAtRuneBoundary(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if limit >= len(s) {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

func hasUsageData(usage GenAIUsage) bool {
	return usage.InputTokens != 0 ||
		usage.OutputTokens != 0 ||
		usage.CostUSD != 0 ||
		usage.AudioSeconds != 0 ||
		usage.ImageCount != 0
}

func normalizePurpose(purpose string) string {
	if purpose == "" {
		return PurposeGeneration
	}
	return purpose
}

func normalizeCurrency(currency string) string {
	if currency == "" {
		return defaultCostCurrency
	}
	return currency
}

// RecordInteraction records the payload and usage for a single GenAI interaction.
func RecordInteraction(ctx context.Context, span trace.Span, payload GenAIPayload, usage GenAIUsage) {
	cfg := currentConfig()
	if cfg.RecordPayloads() {
		payloadAttrs := make([]attribute.KeyValue, 0, 3)
		if payload.System != "" {
			payloadAttrs = append(payloadAttrs, SystemKey.String(truncateContextWithConfig(payload.System, cfg)))
		}
		if payload.Prompt != "" {
			payloadAttrs = append(payloadAttrs, PromptKey.String(truncateContextWithConfig(payload.Prompt, cfg)))
		}
		if payload.Completion != "" {
			payloadAttrs = append(payloadAttrs, CompletionKey.String(truncateContextWithConfig(payload.Completion, cfg)))
		}
		if len(payloadAttrs) > 0 {
			span.SetAttributes(payloadAttrs...)
		}
	}

	if !hasUsageData(usage) {
		return
	}

	purpose := normalizePurpose(usage.Purpose)
	currency := normalizeCurrency(usage.Currency)
	usageAttrs := []attribute.KeyValue{
		InputTokensKey.Int(usage.InputTokens),
		OutputTokensKey.Int(usage.OutputTokens),
		CostUSDKey.Float64(usage.CostUSD),
		CostCurrencyKey.String(currency),
		OperationPurposeKey.String(purpose),
	}
	if usage.AudioSeconds > 0 {
		usageAttrs = append(usageAttrs, AudioSecondsKey.Float64(usage.AudioSeconds))
	}
	if usage.ImageCount > 0 {
		usageAttrs = append(usageAttrs, ImageCountKey.Int(usage.ImageCount))
	}
	span.SetAttributes(usageAttrs...)

	tokenOpts := metric.WithAttributes(OperationPurposeKey.String(purpose))
	holder := currentMetricsHolder()
	if holder != nil {
		if holder.InputTokens != nil {
			holder.InputTokens.Add(ctx, int64(usage.InputTokens), tokenOpts)
		}
		if holder.OutputTokens != nil {
			holder.OutputTokens.Add(ctx, int64(usage.OutputTokens), tokenOpts)
		}
		if holder.Cost != nil && usage.CostUSD != 0 {
			costOpts := metric.WithAttributes(
				OperationPurposeKey.String(purpose),
				CostCurrencyKey.String(currency),
			)
			holder.Cost.Add(ctx, usage.CostUSD, costOpts)
		}
	}
}

// StartToolSpan creates a child span for a tool invocation. Caller MUST call span.End() (e.g. via defer).
// Use for parallel tool calls so each has its own span and timing.
func StartToolSpan(ctx context.Context, toolName, toolID, argsJSON string) (context.Context, trace.Span) {
	cfg := currentConfig()
	ctx, span := getTracer().Start(ctx, "tool: "+toolName)
	span.SetAttributes(
		ToolNameKey.String(toolName),
		ToolIDKey.String(toolID),
		ToolArgsKey.String(truncateContextWithConfig(argsJSON, cfg)),
	)
	return ctx, span
}

// RecordToolResult records the result of a tool call on its own span (from StartToolSpan).
// resultJSON is truncated; isError sets span status to Error for dashboard visibility.
func RecordToolResult(span trace.Span, resultJSON string, isError bool) {
	span.SetAttributes(ToolResultKey.String(truncateContextWithConfig(resultJSON, currentConfig())))
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
// Event name and attributes follow OTel GenAI semantic conventions for dashboards.
// Call from flowy on each state transition; multiple calls on the same span produce a chronological event list.
func RecordAgentStep(span trace.Span, agentName, agentRole, step string) {
	span.AddEvent(AgentStepEvent, trace.WithAttributes(
		AgentNameKey.String(agentName),
		AgentRoleKey.String(agentRole),
		WorkflowStepKey.String(step),
	))
}
