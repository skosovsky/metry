package genai

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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

// RecordInteraction records one GenAI interaction on an explicit tracker.
func (t *Tracker) RecordInteraction(
	ctx context.Context,
	span trace.Span,
	meta Meta,
	payload Payload,
	usage Usage,
) {
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

	t.recordUsageMetrics(ctx, meta, usage)
	t.recordOperationDuration(ctx, meta)
}

func buildMetaAttributes(meta Meta) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, attrCapMetaAttrs)
	if meta.Provider != "" {
		attrs = append(attrs, ProviderNameKey.String(meta.Provider))
	}
	if meta.Operation != "" {
		attrs = append(attrs, OperationNameKey.String(meta.Operation))
	}
	if meta.RequestModel != "" {
		attrs = append(attrs, RequestModelKey.String(meta.RequestModel))
	}
	if meta.ResponseModel != "" {
		attrs = append(attrs, ResponseModelKey.String(meta.ResponseModel))
	}
	if meta.ServerAddress != "" {
		attrs = append(attrs, ServerAddressKey.String(meta.ServerAddress))
	}
	if meta.ServerPort > 0 {
		attrs = append(attrs, ServerPortKey.Int(meta.ServerPort))
	}
	if meta.ErrorType != "" {
		attrs = append(attrs, ErrorTypeKey.String(meta.ErrorType))
	}
	return attrs
}

func buildPayloadAttributes(payload Payload, cfg runtimeConfig) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, attrCapPayloadAttrs)
	if value := marshalPayloadValue(payload.SystemInstructions, cfg); value != "" {
		attrs = append(attrs, SystemInstructionsKey.String(value))
	}
	if value := marshalPayloadValue(payload.InputMessages, cfg); value != "" {
		attrs = append(attrs, InputMessagesKey.String(value))
	}
	if value := marshalPayloadValue(payload.OutputMessages, cfg); value != "" {
		attrs = append(attrs, OutputMessagesKey.String(value))
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
		attrs = append(attrs, InputTokensKey.Int(usage.InputTokens))
	}
	if usage.OutputTokens > 0 {
		attrs = append(attrs, OutputTokensKey.Int(usage.OutputTokens))
	}
	if usage.CacheCreationInputTokens > 0 {
		attrs = append(attrs, CacheCreationInputTokensKey.Int(usage.CacheCreationInputTokens))
	}
	if usage.CacheReadInputTokens > 0 {
		attrs = append(attrs, CacheReadInputTokensKey.Int(usage.CacheReadInputTokens))
	}
	if usage.ReasoningOutputTokens > 0 {
		attrs = append(attrs, UsageReasoningOutputTokensKey.Int(usage.ReasoningOutputTokens))
	}
	if usage.AudioSeconds > 0 {
		attrs = append(attrs, AudioSecondsKey.Float64(usage.AudioSeconds))
	}
	if usage.ImageCount > 0 {
		attrs = append(attrs, ImageCountKey.Int(usage.ImageCount))
	}
	if usage.VideoSeconds > 0 {
		attrs = append(attrs, UsageVideoSecondsKey.Float64(usage.VideoSeconds))
	}
	if usage.VideoFrames > 0 {
		attrs = append(attrs, UsageVideoFramesKey.Int(usage.VideoFrames))
	}
	if usage.Cost > 0 {
		attrs = append(attrs,
			UsageCostKey.Float64(usage.Cost),
			CostCurrencyKey.String(normalizeCurrency(usage.Currency)),
			OperationPurposeKey.String(normalizePurpose(usage.Purpose)),
		)
	} else if usage.Purpose != "" {
		attrs = append(attrs, OperationPurposeKey.String(usage.Purpose))
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
			appendAttribute(baseAttrs, TokenTypeKey.String(TokenTypeInput)),
		)
		recordIntHistogram(
			ctx,
			t.metrics.TokenUsage,
			usage.OutputTokens,
			appendAttribute(baseAttrs, TokenTypeKey.String(TokenTypeOutput)),
		)
	}
	if ok && t.metrics.TokenComponentUsage != nil {
		recordIntHistogram(
			ctx,
			t.metrics.TokenComponentUsage,
			usage.CacheCreationInputTokens,
			appendAttribute(baseAttrs, TokenTypeKey.String(TokenTypeInputCacheCreation)),
		)
		recordIntHistogram(
			ctx,
			t.metrics.TokenComponentUsage,
			usage.CacheReadInputTokens,
			appendAttribute(baseAttrs, TokenTypeKey.String(TokenTypeInputCacheRead)),
		)
		recordIntHistogram(
			ctx,
			t.metrics.TokenComponentUsage,
			usage.ReasoningOutputTokens,
			appendAttribute(baseAttrs, TokenTypeKey.String(TokenTypeOutputReasoning)),
		)
	}

	customAttrs := append([]attribute.KeyValue{}, baseAttrs...)
	customAttrs = append(customAttrs, OperationPurposeKey.String(normalizePurpose(usage.Purpose)))
	customAttrs = append(customAttrs, CostCurrencyKey.String(normalizeCurrency(usage.Currency)))

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
		attrs = append(attrs, ErrorTypeKey.String(meta.ErrorType))
	}
	t.metrics.OperationDuration.Record(ctx, meta.Duration.Seconds(), metric.WithAttributes(attrs...))
}

func metricAttributesFromMeta(meta Meta) ([]attribute.KeyValue, bool) {
	if meta.Provider == "" || meta.Operation == "" {
		return nil, false
	}
	attrs := []attribute.KeyValue{
		ProviderNameKey.String(meta.Provider),
		OperationNameKey.String(meta.Operation),
	}
	if meta.RequestModel != "" {
		attrs = append(attrs, RequestModelKey.String(meta.RequestModel))
	}
	if meta.ResponseModel != "" {
		attrs = append(attrs, ResponseModelKey.String(meta.ResponseModel))
	}
	if meta.ServerAddress != "" {
		attrs = append(attrs, ServerAddressKey.String(meta.ServerAddress))
	}
	if meta.ServerPort > 0 {
		attrs = append(attrs, ServerPortKey.Int(meta.ServerPort))
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

// StartToolSpan creates a child span for a tool execution using an explicit tracker.
// Extra start options allow callers to add start-time sampling hints or attributes.
//
//nolint:spancheck // The span is returned to the caller, which is responsible for ending it.
func (t *Tracker) StartToolSpan(
	ctx context.Context,
	toolName, toolCallID, argsJSON string,
	startOpts ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{
		OperationNameKey.String("execute_tool"),
		ToolNameKey.String(toolName),
		ToolCallIDKey.String(toolCallID),
	}
	if argsJSON != "" {
		attrs = append(attrs, ToolCallArgumentsKey.String(truncateContextWithConfig(argsJSON, t.cfg)))
	}
	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	opts = append(opts, startOpts...)
	ctx, span := t.tracer.Start(ctx, "tool: "+toolName, opts...)
	// Preserve deterministic helper semantics when caller start options use duplicate keys.
	span.SetAttributes(attrs...)
	return ctx, span
}

// RecordToolResult records tool output and status using an explicit tracker config.
func (t *Tracker) RecordToolResult(span trace.Span, resultJSON string, isError bool) {
	if resultJSON != "" {
		span.SetAttributes(ToolCallResultKey.String(truncateContextWithConfig(resultJSON, t.cfg)))
	}
	if isError {
		span.SetAttributes(ToolErrorKey.Bool(true))
		span.SetStatus(codes.Error, "tool execution failed")
		return
	}
	span.SetAttributes(ToolErrorKey.Bool(false))
	span.SetStatus(codes.Ok, "")
}

// RecordCacheHit records cache metadata on the provided span.
func RecordCacheHit(span trace.Span, hit bool, source string) {
	span.SetAttributes(
		attribute.Bool(CacheHit, hit),
		attribute.String(RetrievalSource, source),
	)
}

// RecordAgentStep appends one agent-step event on the provided span.
func RecordAgentStep(span trace.Span, agentName, agentRole, step string) {
	span.AddEvent(AgentStepEvent, trace.WithAttributes(
		AgentNameKey.String(agentName),
		AgentRoleKey.String(agentRole),
		WorkflowStepKey.String(step),
	))
}
