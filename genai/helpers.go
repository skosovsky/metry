package genai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"
)

const truncationSuffix = "... [truncated]"
const defaultCostCurrency = "USD"

// Preallocated capacities for attribute slices built in hot paths.
const (
	attrCapInteractionAttrs = 16
	attrCapMetaAttrs        = 7
	attrCapPayloadAttrs     = 3
	attrCapUsageAttrs       = 10
)

// Meta describes operation-level metadata used for official GenAI semconv.
type Meta struct {
	Provider      string
	Operation     string
	Purpose       string
	RequestModel  string
	ResponseModel string
	ServerAddress string
	ServerPort    int
	Duration      time.Duration
	ErrorType     string
}

// ContentPart is one structured content block inside a system instruction or message.
type ContentPart struct {
	Type      string          `json:"type"`
	Content   string          `json:"content,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// Message is one structured GenAI message emitted on spans as JSON payload.
type Message struct {
	Role         string        `json:"role"`
	Parts        []ContentPart `json:"parts"`
	FinishReason string        `json:"finish_reason,omitempty"`
}

// Payload captures structured system/input/output content for one interaction.
type Payload struct {
	SystemInstructions []ContentPart
	InputMessages      []Message
	OutputMessages     []Message
}

// Usage captures billable and multimodal usage for one interaction.
//
// Flat design: fields are kept in one struct (no nested DTOs or pointers to sub-structs), which minimizes
// allocations and GC work on hot paths compared to nested DTOs (zero-allocation-oriented layout, not a guarantee
// that the surrounding call path allocates nothing). The shape maps 1:1 to flat OpenTelemetry GenAI semantic
// attributes without embedding nested JSON. Zero values (0, "") mean "not set" and are the fastest idiom for
// presence checks.
//
// InputTokens should include cached input tokens when the provider exposes a total.
// OutputTokens should include reasoning output tokens when the provider exposes a total.
// Cost must be non-negative; negative values are treated as invalid input and ignored.
type Usage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	ReasoningOutputTokens    int
	Cost                     float64
	Currency                 string
	AudioSeconds             float64
	ImageCount               int
	VideoSeconds             float64
	VideoFrames              int
	Purpose                  string
}

func truncateContextWithConfig(s string, cfg runtimeConfig) string {
	return truncateContextWithLimit(s, cfg.MaxContextLength())
}

func truncateContextWithLimit(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return strings.ToValidUTF8(s, "")
	}
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

func hasUsageData(usage Usage) bool {
	return usage.InputTokens != 0 ||
		usage.OutputTokens != 0 ||
		usage.CacheCreationInputTokens != 0 ||
		usage.CacheReadInputTokens != 0 ||
		usage.ReasoningOutputTokens != 0 ||
		usage.Cost > 0 ||
		usage.AudioSeconds != 0 ||
		usage.ImageCount != 0 ||
		usage.VideoSeconds != 0 ||
		usage.VideoFrames != 0 ||
		usage.Purpose != ""
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

// RecordInteraction records one GenAI interaction, creating a span when needed.
func (t *Tracker) RecordInteraction(
	ctx context.Context,
	meta Meta,
	payload Payload,
	usage Usage,
) error {
	scope, hasScope := ScopeFromContext(ctx)
	meta = mergeMetaFromScopeWithScope(meta, scope, hasScope)
	spanName := interactionSpanNameFromMeta(meta, scope, hasScope)
	ctx, span := t.tracer.Start(ctx, spanName)
	defer span.End()
	t.recordInteractionOnSpan(ctx, span, meta, payload, usage)
	return nil
}

func (t *Tracker) recordInteractionOnSpan(
	ctx context.Context,
	span trace.Span,
	meta Meta,
	payload Payload,
	usage Usage,
) {
	if usage.Purpose == "" && meta.Purpose != "" {
		usage.Purpose = meta.Purpose
	}
	attrs := make([]attribute.KeyValue, 0, attrCapInteractionAttrs)
	attrs = append(attrs, buildMetaAttributes(meta)...)
	if t.cfg.RecordPayloads() {
		attrs = append(attrs, buildPayloadAttributes(payload, t.cfg)...)
	}
	if hasUsageData(usage) {
		attrs = append(attrs, buildUsageAttributes(usage)...)
	}
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	if meta.ErrorType != "" {
		traceutil.SpanError(span, errors.New(meta.ErrorType))
	} else {
		traceutil.SpanOK(span)
	}

	t.recordUsageMetrics(ctx, meta, usage)
	t.recordOperationDuration(ctx, meta)
}

func interactionSpanNameFromMeta(meta Meta, scope Scope, hasScope bool) string {
	if meta.Operation != "" {
		return meta.Operation
	}
	if hasScope && scope.Operation != "" {
		return scope.Operation
	}
	return "genai.interaction"
}

func buildMetaAttributes(meta Meta) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, attrCapMetaAttrs)
	if meta.Provider != "" {
		attrs = append(attrs, attribute.Key(ProviderName).String(meta.Provider))
	}
	if meta.Operation != "" {
		attrs = append(attrs, attribute.Key(OperationName).String(meta.Operation))
	}
	if meta.RequestModel != "" {
		attrs = append(attrs, attribute.Key(RequestModel).String(meta.RequestModel))
	}
	if meta.ResponseModel != "" {
		attrs = append(attrs, attribute.Key(ResponseModel).String(meta.ResponseModel))
	}
	if meta.ServerAddress != "" {
		attrs = append(attrs, attribute.Key(ServerAddress).String(meta.ServerAddress))
	}
	if meta.ServerPort > 0 {
		attrs = append(attrs, attribute.Key(ServerPort).Int(meta.ServerPort))
	}
	if meta.ErrorType != "" {
		attrs = append(attrs, attribute.Key(ErrorType).String(meta.ErrorType))
	}
	if meta.Purpose != "" {
		attrs = append(attrs, attribute.Key(OperationPurpose).String(meta.Purpose))
	}
	return attrs
}

func buildPayloadAttributes(payload Payload, cfg runtimeConfig) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, attrCapPayloadAttrs)
	if value := marshalPayloadValue(payload.SystemInstructions, cfg); value != "" {
		attrs = append(attrs, attribute.Key(SystemInstructions).String(value))
	}
	if value := marshalPayloadValue(payload.InputMessages, cfg); value != "" {
		attrs = append(attrs, attribute.Key(InputMessages).String(value))
	}
	if value := marshalPayloadValue(payload.OutputMessages, cfg); value != "" {
		attrs = append(attrs, attribute.Key(OutputMessages).String(value))
	}
	return attrs
}

func marshalPayloadValue(value any, cfg runtimeConfig) string {
	if value == nil {
		return ""
	}
	buf, err := json.Marshal(value)
	if err != nil || string(buf) == "null" || string(buf) == "[]" {
		return ""
	}
	return truncateContextWithConfig(string(buf), cfg)
}

func buildUsageAttributes(usage Usage) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, attrCapUsageAttrs)
	if usage.InputTokens > 0 {
		attrs = append(attrs, attribute.Key(InputTokens).Int(usage.InputTokens))
	}
	if usage.OutputTokens > 0 {
		attrs = append(attrs, attribute.Key(OutputTokens).Int(usage.OutputTokens))
	}
	if usage.CacheCreationInputTokens > 0 {
		attrs = append(attrs, attribute.Key(CacheCreationInputTokens).Int(usage.CacheCreationInputTokens))
	}
	if usage.CacheReadInputTokens > 0 {
		attrs = append(attrs, attribute.Key(CacheReadInputTokens).Int(usage.CacheReadInputTokens))
	}
	if usage.ReasoningOutputTokens > 0 {
		attrs = append(attrs, attribute.Key(UsageReasoningOutputTokens).Int(usage.ReasoningOutputTokens))
	}
	if usage.AudioSeconds > 0 {
		attrs = append(attrs, attribute.Key(AudioSeconds).Float64(usage.AudioSeconds))
	}
	if usage.ImageCount > 0 {
		attrs = append(attrs, attribute.Key(ImageCount).Int(usage.ImageCount))
	}
	if usage.VideoSeconds > 0 {
		attrs = append(attrs, attribute.Key(UsageVideoSeconds).Float64(usage.VideoSeconds))
	}
	if usage.VideoFrames > 0 {
		attrs = append(attrs, attribute.Key(UsageVideoFrames).Int(usage.VideoFrames))
	}
	if usage.Cost > 0 {
		attrs = append(attrs,
			attribute.Key(UsageCost).Float64(usage.Cost),
			attribute.Key(CostCurrency).String(normalizeCurrency(usage.Currency)),
			attribute.Key(OperationPurpose).String(normalizePurpose(usage.Purpose)),
		)
	} else if usage.Purpose != "" {
		attrs = append(attrs, attribute.Key(OperationPurpose).String(usage.Purpose))
	}
	return attrs
}

func (t *Tracker) recordUsageMetrics(ctx context.Context, meta Meta, usage Usage) {
	if t.metrics == nil || !hasUsageData(usage) {
		return
	}

	baseAttrs, ok := metricAttributesFromMeta(meta)
	if ok && t.metrics.TokenUsage != nil {
		recordIntHistogram(
			ctx,
			t.metrics.TokenUsage,
			usage.InputTokens,
			appendAttribute(baseAttrs, attribute.Key(TokenType).String(TokenTypeInput)),
		)
		recordIntHistogram(
			ctx,
			t.metrics.TokenUsage,
			usage.OutputTokens,
			appendAttribute(baseAttrs, attribute.Key(TokenType).String(TokenTypeOutput)),
		)
	}
	if ok && t.metrics.TokenComponentUsage != nil {
		recordIntHistogram(
			ctx,
			t.metrics.TokenComponentUsage,
			usage.CacheCreationInputTokens,
			appendAttribute(baseAttrs, attribute.Key(TokenType).String(TokenTypeInputCacheCreation)),
		)
		recordIntHistogram(
			ctx,
			t.metrics.TokenComponentUsage,
			usage.CacheReadInputTokens,
			appendAttribute(baseAttrs, attribute.Key(TokenType).String(TokenTypeInputCacheRead)),
		)
		recordIntHistogram(
			ctx,
			t.metrics.TokenComponentUsage,
			usage.ReasoningOutputTokens,
			appendAttribute(baseAttrs, attribute.Key(TokenType).String(TokenTypeOutputReasoning)),
		)
	}

	customAttrs := append([]attribute.KeyValue{}, baseAttrs...)
	customAttrs = append(customAttrs, attribute.Key(OperationPurpose).String(normalizePurpose(usage.Purpose)))
	customAttrs = append(customAttrs, attribute.Key(CostCurrency).String(normalizeCurrency(usage.Currency)))

	if t.metrics.Cost != nil && usage.Cost > 0 {
		t.metrics.Cost.Add(ctx, usage.Cost, metric.WithAttributes(customAttrs...))
	}
	if t.metrics.VideoSeconds != nil && usage.VideoSeconds > 0 {
		t.metrics.VideoSeconds.Record(ctx, usage.VideoSeconds, metric.WithAttributes(baseAttrs...))
	}
	if t.metrics.VideoFrames != nil && usage.VideoFrames > 0 {
		t.metrics.VideoFrames.Record(ctx, int64(usage.VideoFrames), metric.WithAttributes(baseAttrs...))
	}
}

func (t *Tracker) recordOperationDuration(ctx context.Context, meta Meta) {
	if t.metrics == nil || t.metrics.OperationDuration == nil || meta.Duration <= 0 {
		return
	}
	attrs, ok := metricAttributesFromMeta(meta)
	if !ok {
		return
	}
	if meta.ErrorType != "" {
		attrs = append(attrs, attribute.Key(ErrorType).String(meta.ErrorType))
	}
	t.metrics.OperationDuration.Record(ctx, meta.Duration.Seconds(), metric.WithAttributes(attrs...))
}

func metricAttributesFromMeta(meta Meta) ([]attribute.KeyValue, bool) {
	if meta.Provider == "" || meta.Operation == "" {
		return nil, false
	}
	attrs := []attribute.KeyValue{
		attribute.Key(ProviderName).String(meta.Provider),
		attribute.Key(OperationName).String(meta.Operation),
	}
	if meta.RequestModel != "" {
		attrs = append(attrs, attribute.Key(RequestModel).String(meta.RequestModel))
	}
	if meta.ResponseModel != "" {
		attrs = append(attrs, attribute.Key(ResponseModel).String(meta.ResponseModel))
	}
	if meta.ServerAddress != "" {
		attrs = append(attrs, attribute.Key(ServerAddress).String(meta.ServerAddress))
	}
	if meta.ServerPort > 0 {
		attrs = append(attrs, attribute.Key(ServerPort).Int(meta.ServerPort))
	}
	return attrs, true
}

func appendAttribute(attrs []attribute.KeyValue, attr attribute.KeyValue) []attribute.KeyValue {
	next := make([]attribute.KeyValue, 0, len(attrs)+1)
	next = append(next, attrs...)
	next = append(next, attr)
	return next
}

func recordIntHistogram(ctx context.Context, histogram metric.Int64Histogram, value int, attrs []attribute.KeyValue) {
	if histogram == nil || value <= 0 {
		return
	}
	histogram.Record(ctx, int64(value), metric.WithAttributes(attrs...))
}

// StartToolSpan creates a child span for a tool execution and returns an end callback.
// Call RecordToolResult before end() for explicit I/O completion semantics.
// The end callback sets Ok when span status is still Unset.
// Extra start options allow callers to add start-time sampling hints or attributes.
func (t *Tracker) StartToolSpan(
	ctx context.Context,
	toolName, toolCallID, argsJSON string,
	startOpts ...ChildSpanOption,
) (context.Context, func()) {
	attrs := []attribute.KeyValue{
		attribute.Key(OperationName).String("execute_tool"),
		attribute.Key(ToolName).String(toolName),
		attribute.Key(ToolCallID).String(toolCallID),
	}
	if argsJSON != "" {
		attrs = append(attrs, attribute.Key(ToolCallArguments).String(truncateContextWithConfig(argsJSON, t.cfg)))
	}
	otelOpts := childSpanOptionsToOTel(startOpts...)
	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	opts = append(opts, otelOpts...)
	ctx, span := t.tracer.Start(ctx, "tool: "+toolName, opts...) //nolint:spancheck // caller ends span via callback
	// Preserve deterministic helper semantics when caller start options use duplicate keys.
	span.SetAttributes(attrs...)
	return ctx, func() { traceutil.EndSpanOKIfUnset(span) } //nolint:spancheck // caller ends span via callback
}

// RecordToolResult records tool output and status on the span stored in ctx.
func (t *Tracker) RecordToolResult(ctx context.Context, resultJSON string, err error) {
	mutateSpan(ctx, func(span trace.Span) {
		if resultJSON != "" {
			span.SetAttributes(attribute.Key(ToolCallResult).String(truncateContextWithConfig(resultJSON, t.cfg)))
		}
		if err != nil {
			span.SetAttributes(attribute.Key(ToolError).Bool(true))
			traceutil.SpanError(span, err)
			return
		}
		span.SetAttributes(attribute.Key(ToolError).Bool(false))
		traceutil.SpanOK(span)
	})
}

// RecordCacheHit records cache metadata on the span stored in ctx.
func (t *Tracker) RecordCacheHit(ctx context.Context, hit bool, source string) {
	mutateSpan(ctx, func(span trace.Span) {
		span.SetAttributes(
			attribute.Bool(CacheHit, hit),
			attribute.String(RetrievalSource, source),
		)
	})
}

// RecordAgentStep appends one agent-step event on the span stored in ctx.
func (t *Tracker) RecordAgentStep(ctx context.Context, agentName, agentRole, step string) {
	mutateSpan(ctx, func(span trace.Span) {
		span.AddEvent(AgentStepEvent, trace.WithAttributes(
			attribute.Key(AgentName).String(agentName),
			attribute.Key(AgentRole).String(agentRole),
			attribute.Key(WorkflowStep).String(step),
		))
	})
}
