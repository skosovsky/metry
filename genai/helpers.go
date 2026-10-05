package genai

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const truncationSuffix = "... [truncated]"

// Preallocated capacities for attribute slices built in hot paths.
const (
	attrCapInteractionAttrs = 16
	attrCapMetaAttrs        = 7
	attrCapPayloadAttrs     = 3
	attrCapUsageAttrs       = 10
)

// Meta describes operation-level metadata used for official GenAI semconv.
type Meta struct {
	Status        Outcome
	Provider      string
	Operation     string
	Purpose       string
	RequestModel  string
	ResponseModel string
	ServerAddress string
	ServerPort    int
	Duration      time.Duration
	ErrorType     string
	interval      bool
	locality      ExecutionLocality
}

// ContentPart is one structured content block inside a system instruction or message.
type ContentPart struct {
	Type      string          `json:"type"`
	Content   string          `json:"content"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Response  json.RawMessage `json:"response"`
}

// Message is one structured GenAI message emitted on spans as JSON payload.
type Message struct {
	Role  string        `json:"role"`
	Parts []ContentPart `json:"parts"`
}

// MarshalJSON keeps a present message's parts an array, including empty messages.
func (message Message) MarshalJSON() ([]byte, error) {
	type wire Message
	if message.Parts == nil {
		message.Parts = []ContentPart{}
	}
	return json.Marshal(wire(message))
}

// Payload captures structured system/input/output content for one interaction.
type Payload struct {
	SystemInstructions []ContentPart
	InputMessages      []Message
	OutputMessages     []Message
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
	return usage.InputTokens.Present || usage.OutputTokens.Present ||
		usage.CacheWriteInputTokens.Present || usage.CacheReadInputTokens.Present ||
		usage.ReasoningOutputTokens.Present || usage.Cost.Present ||
		usage.AudioSeconds.Present || usage.ImageCount.Present ||
		usage.VideoSeconds.Present || usage.VideoFrames.Present
}

func normalizePurpose(purpose string) string {
	if purpose == "" {
		return PurposeGeneration
	}
	return purpose
}

// RecordInteraction records one GenAI interaction, creating a span when needed.
func (t *Tracker) RecordInteraction(
	ctx context.Context,
	meta Meta,
	payload Payload,
	usage Usage,
) error {
	status, class, outcomeErr := resolveModelOutcome(meta.Status, meta.ErrorType)
	if outcomeErr != nil {
		return outcomeErr
	}
	meta.Status = Outcome(status)
	meta.ErrorType = class
	if err := t.validateUsage(usage); err != nil {
		return err
	}
	if meta.Duration < 0 {
		return invalidObservation("duration", "invalid_interval")
	}
	scope, hasScope := ScopeFromContext(ctx)
	meta = mergeMetaFromScopeWithScope(meta, scope, hasScope)
	if err := validateModelMetadata(meta.RequestModel, meta.Purpose); err != nil {
		return err
	}
	if err := validateModelMetadata(meta.ResponseModel, ""); err != nil {
		return err
	}
	if err := validateOperationName(meta.Operation); err != nil {
		return err
	}
	payloadAttrs, err := buildPayloadAttributes(payload, t.cfg)
	if err != nil {
		return err
	}
	spanName := modelSpanName(meta, t.cfg)
	ctx, span := t.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(attribute.String("metry.observation.kind", "completed"))
	t.recordInteractionOnSpan(ctx, span, meta, payloadAttrs, usage, true)
	return nil
}

func (t *Tracker) recordInteractionOnSpan(
	ctx context.Context,
	span trace.Span,
	meta Meta,
	payloadAttrs []attribute.KeyValue,
	usage Usage,
	accounting bool,
) {
	if usage.Purpose == "" && meta.Purpose != "" {
		usage.Purpose = meta.Purpose
	}
	attrs := make([]attribute.KeyValue, 0, attrCapInteractionAttrs)
	attrs = append(attrs, buildMetaAttributes(meta, t.cfg)...)
	attrs = append(attrs, attribute.String(OperationStatus, string(meta.Status)))
	attrs = append(attrs, payloadAttrs...)
	if hasUsageData(usage) {
		attrs = append(attrs, buildUsageAttributes(usage, t.cfg)...)
	}
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	applyOutcome(span, string(meta.Status), meta.ErrorType)

	if accounting {
		t.recordUsageMetrics(ctx, meta, usage)
		t.recordOperationDuration(ctx, meta)
	}
}

func buildMetaAttributes(meta Meta, cfg runtimeConfig) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, attrCapMetaAttrs)
	if meta.Provider != "" {
		attrs = append(attrs, attribute.Key(ProviderName).String(metadataWithConfig(ProviderName, meta.Provider, cfg)))
	}
	if meta.Operation != "" {
		attrs = append(
			attrs,
			attribute.Key(OperationName).String(metadataWithConfig(OperationName, meta.Operation, cfg)),
		)
	}
	if meta.RequestModel != "" {
		attrs = append(
			attrs,
			attribute.Key(RequestModel).String(metadataWithConfig(RequestModel, meta.RequestModel, cfg)),
		)
	}
	if meta.ResponseModel != "" {
		attrs = append(
			attrs,
			attribute.Key(ResponseModel).String(metadataWithConfig(ResponseModel, meta.ResponseModel, cfg)),
		)
	}
	if meta.ServerAddress != "" {
		attrs = append(
			attrs,
			attribute.Key(ServerAddress).String(metadataWithConfig(ServerAddress, meta.ServerAddress, cfg)),
		)
	}
	if meta.ServerPort > 0 {
		attrs = append(attrs, attribute.Key(ServerPort).Int(meta.ServerPort))
	}
	if meta.ErrorType != "" {
		attrs = append(attrs, attribute.Key(ErrorType).String(meta.ErrorType))
	}
	if meta.Purpose != "" {
		attrs = append(
			attrs,
			attribute.Key(OperationPurpose).String(metadataWithConfig(OperationPurpose, meta.Purpose, cfg)),
		)
	}
	return attrs
}

func buildPayloadAttributes(payload Payload, cfg runtimeConfig) ([]attribute.KeyValue, error) {
	if !cfg.RecordPayloads() {
		return nil, nil
	}
	if err := validatePayloadBounds(payload); err != nil {
		return nil, err
	}
	payload = cfg.PayloadPolicy().SanitizePayload(payload)
	if err := validatePayloadEncodingBounds(payload, false); err != nil {
		return nil, err
	}
	attrs := make([]attribute.KeyValue, 0, attrCapPayloadAttrs)
	for _, item := range []struct {
		key   string
		value any
	}{
		{SystemInstructions, payload.SystemInstructions},
		{InputMessages, payload.InputMessages},
		{OutputMessages, payload.OutputMessages},
	} {
		value, truncated, err := boundedJSONArray(item.value, cfg.MaxContextLength())
		if err != nil {
			return nil, err
		}
		if value != "" {
			attrs = append(attrs, attribute.String(item.key, value))
			if truncated {
				attrs = append(attrs, attribute.Bool(payloadTruncationKey(item.key), true))
			}
		}
	}
	return attrs, nil
}

func buildUsageAttributes(usage Usage, cfg runtimeConfig) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, attrCapUsageAttrs)
	if usage.InputTokens.Present {
		attrs = append(attrs, attribute.Key(InputTokens).Int(usage.InputTokens.Value))
	}
	if usage.OutputTokens.Present {
		attrs = append(attrs, attribute.Key(OutputTokens).Int(usage.OutputTokens.Value))
	}
	if usage.CacheWriteInputTokens.Present {
		attrs = append(attrs, attribute.Key(CacheWriteInputTokens).Int(usage.CacheWriteInputTokens.Value))
	}
	if usage.CacheReadInputTokens.Present {
		attrs = append(attrs, attribute.Key(CacheReadInputTokens).Int(usage.CacheReadInputTokens.Value))
	}
	if usage.ReasoningOutputTokens.Present {
		attrs = append(attrs, attribute.Key(UsageReasoningOutputTokens).Int(usage.ReasoningOutputTokens.Value))
	}
	if usage.AudioSeconds.Present {
		attrs = append(attrs, attribute.Key(AudioSeconds).Float64(usage.AudioSeconds.Value))
	}
	if usage.ImageCount.Present {
		attrs = append(attrs, attribute.Key(ImageCount).Int(usage.ImageCount.Value))
	}
	if usage.VideoSeconds.Present {
		attrs = append(attrs, attribute.Key(UsageVideoSeconds).Float64(usage.VideoSeconds.Value))
	}
	if usage.VideoFrames.Present {
		attrs = append(attrs, attribute.Key(UsageVideoFrames).Int(usage.VideoFrames.Value))
	}
	if usage.Cost.Present {
		attrs = append(
			attrs,
			attribute.Key(UsageCost).Float64(usage.Cost.Value),
			attribute.Key(CostCurrency).String(usage.Currency),
			attribute.Key(OperationPurpose).
				String(metadataWithConfig(OperationPurpose, normalizePurpose(usage.Purpose), cfg)),
		)
	} else if usage.Purpose != "" {
		purpose := metadataWithConfig(OperationPurpose, usage.Purpose, cfg)
		attrs = append(attrs, attribute.String(OperationPurpose, purpose))
	}
	if usage.PriceReference != "" {
		attrs = append(
			attrs,
			attribute.String("metry.usage.price_reference", referenceWithConfig(usage.PriceReference, cfg)),
		)
	}
	for _, item := range []struct {
		key     string
		present bool
		source  Source
		unit    Unit
	}{
		{InputTokens, usage.InputTokens.Present, usage.InputTokens.Source, usage.InputTokens.Unit},
		{OutputTokens, usage.OutputTokens.Present, usage.OutputTokens.Source, usage.OutputTokens.Unit},
		{CacheWriteInputTokens, usage.CacheWriteInputTokens.Present, usage.CacheWriteInputTokens.Source, usage.CacheWriteInputTokens.Unit},
		{CacheReadInputTokens, usage.CacheReadInputTokens.Present, usage.CacheReadInputTokens.Source, usage.CacheReadInputTokens.Unit},
		{UsageReasoningOutputTokens, usage.ReasoningOutputTokens.Present, usage.ReasoningOutputTokens.Source, usage.ReasoningOutputTokens.Unit},
		{AudioSeconds, usage.AudioSeconds.Present, usage.AudioSeconds.Source, usage.AudioSeconds.Unit},
		{ImageCount, usage.ImageCount.Present, usage.ImageCount.Source, usage.ImageCount.Unit},
		{UsageVideoSeconds, usage.VideoSeconds.Present, usage.VideoSeconds.Source, usage.VideoSeconds.Unit},
		{UsageVideoFrames, usage.VideoFrames.Present, usage.VideoFrames.Source, usage.VideoFrames.Unit},
		{UsageCost, usage.Cost.Present, usage.Cost.Source, usage.Cost.Unit},
	} {
		if item.present {
			attrs = append(
				attrs,
				attribute.String(measurementProvenanceKey(item.key)+".source", string(item.source)),
				attribute.String(measurementProvenanceKey(item.key)+".unit", string(item.unit)),
			)
		}
	}
	if usage.Mapping != "" {
		attrs = append(attrs, attribute.String("metry.usage.mapping", usage.Mapping))
	}
	return attrs
}

func (t *Tracker) recordUsageMetrics(ctx context.Context, meta Meta, usage Usage) {
	if t.metrics == nil || !hasUsageData(usage) {
		return
	}

	baseAttrs, ok := metricAttributesFromMeta(metricMeta(meta, t.cfg))
	if ok && t.metrics.InputTokenUsage != nil {
		t.recordIntHistogram(
			ctx,
			InputTokenUsageMetricName,
			t.metrics.InputTokenUsage,
			usage.InputTokens,
			baseAttrs,
		)
		t.recordIntHistogram(
			ctx,
			OutputTokenUsageMetricName,
			t.metrics.OutputTokenUsage,
			usage.OutputTokens,
			baseAttrs,
		)
	}
	if ok && t.metrics.TokenComponentUsage != nil {
		t.recordIntHistogram(
			ctx,
			TokenComponentUsageMetricName,
			t.metrics.TokenComponentUsage,
			usage.CacheWriteInputTokens,
			appendAttribute(baseAttrs, attribute.Key(TokenType).String(TokenComponentCacheWrite)),
		)
		t.recordIntHistogram(
			ctx,
			TokenComponentUsageMetricName,
			t.metrics.TokenComponentUsage,
			usage.CacheReadInputTokens,
			appendAttribute(baseAttrs, attribute.Key(TokenType).String(TokenComponentCacheRead)),
		)
		t.recordIntHistogram(
			ctx,
			TokenComponentUsageMetricName,
			t.metrics.TokenComponentUsage,
			usage.ReasoningOutputTokens,
			appendAttribute(baseAttrs, attribute.Key(TokenType).String(TokenComponentReasoning)),
		)
	}

	customAttrs := append([]attribute.KeyValue{}, baseAttrs...)
	customAttrs = append(customAttrs, attribute.Key(OperationPurpose).String(normalizePurpose(usage.Purpose)))
	customAttrs = append(customAttrs, attribute.Key(CostCurrency).String(usage.Currency))

	if t.metrics.Cost != nil && usage.Cost.Present {
		costAttrs := appendAttribute(customAttrs, attribute.String("metry.usage.source", string(usage.Cost.Source)))
		t.metrics.Cost.Add(
			ctx,
			usage.Cost.Value,
			metric.WithAttributes(t.boundedMetricAttributes(CostMetricName, costAttrs)...),
		)
	}
	if t.metrics.VideoSeconds != nil && usage.VideoSeconds.Present {
		videoAttrs := appendAttribute(
			baseAttrs,
			attribute.String("metry.usage.source", string(usage.VideoSeconds.Source)),
		)
		t.metrics.VideoSeconds.Record(
			ctx,
			usage.VideoSeconds.Value,
			metric.WithAttributes(t.boundedMetricAttributes(VideoSecondsMetricName, videoAttrs)...),
		)
	}
	if t.metrics.VideoFrames != nil && usage.VideoFrames.Present {
		videoAttrs := appendAttribute(
			baseAttrs,
			attribute.String("metry.usage.source", string(usage.VideoFrames.Source)),
		)
		t.metrics.VideoFrames.Record(
			ctx,
			int64(usage.VideoFrames.Value),
			metric.WithAttributes(t.boundedMetricAttributes(VideoFramesMetricName, videoAttrs)...),
		)
	}
}

func (t *Tracker) recordOperationDuration(ctx context.Context, meta Meta) {
	if t.metrics == nil || meta.Duration <= 0 {
		return
	}
	attrs, ok := metricAttributesFromMeta(metricMeta(meta, t.cfg))
	attrs = append(attrs, attribute.String(OperationStatus, string(meta.Status)))
	if !ok {
		return
	}
	if meta.ErrorType != "" {
		attrs = append(attrs, attribute.Key(ErrorType).String(meta.ErrorType))
	}
	histogram := t.metrics.ObservedOperationDuration
	instrument := ObservedOperationDurationMetricName
	if meta.interval && meta.locality == ExecutionRemote {
		histogram = t.metrics.OperationDuration
		instrument = OperationDurationMetricName
	}
	if histogram == nil {
		return
	}
	histogram.Record(
		ctx,
		meta.Duration.Seconds(),
		metric.WithAttributes(t.boundedMetricAttributes(instrument, attrs)...),
	)
}

func metricAttributesFromMeta(meta Meta) ([]attribute.KeyValue, bool) {
	if meta.Operation == "" {
		return nil, false
	}
	provider := meta.Provider
	if provider == "" {
		provider = unknownProviderName
	}
	attrs := []attribute.KeyValue{
		attribute.Key(ProviderName).String(provider),
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

func (t *Tracker) recordIntHistogram(
	ctx context.Context,
	instrument string,
	histogram metric.Int64Histogram,
	value Measurement[int],
	attrs []attribute.KeyValue,
) {
	if histogram == nil || !value.Present {
		return
	}
	attrs = appendAttribute(attrs, attribute.String("metry.usage.source", string(value.Source)))
	histogram.Record(ctx, int64(value.Value), metric.WithAttributes(t.boundedMetricAttributes(instrument, attrs)...))
}

// RecordCacheHit records cache metadata on the span stored in ctx.
func (t *Tracker) RecordCacheHit(ctx context.Context, hit bool, source string) {
	mutateSpan(ctx, func(span trace.Span) {
		span.SetAttributes(
			attribute.Bool(CacheHit, hit),
			attribute.String(RetrievalSource, referenceWithConfig(source, t.cfg)),
		)
	})
}

// metricMeta applies the same bounded metadata policy to actual metric facts.
func metricMeta(meta Meta, cfg runtimeConfig) Meta {
	meta.Provider = metadataWithConfig(ProviderName, meta.Provider, cfg)
	meta.Operation = metadataWithConfig(OperationName, meta.Operation, cfg)
	if meta.RequestModel != "" {
		meta.RequestModel = metadataWithConfig(RequestModel, meta.RequestModel, cfg)
	}
	if meta.ResponseModel != "" {
		meta.ResponseModel = metadataWithConfig(ResponseModel, meta.ResponseModel, cfg)
	}
	if meta.ServerAddress != "" {
		meta.ServerAddress = metadataWithConfig(ServerAddress, meta.ServerAddress, cfg)
	}
	return meta
}

func modelSpanName(meta Meta, cfg runtimeConfig) string {
	name := metadataWithConfig(OperationName, meta.Operation, cfg)
	if meta.RequestModel != "" {
		name += " " + metadataWithConfig(RequestModel, meta.RequestModel, cfg)
	}
	return name
}

func measurementProvenanceKey(key string) string {
	switch key {
	case InputTokens:
		return "metry.gen_ai.measurement.input_tokens"
	case OutputTokens:
		return "metry.gen_ai.measurement.output_tokens"
	case CacheWriteInputTokens:
		return "metry.gen_ai.measurement.cache_write_input_tokens"
	case CacheReadInputTokens:
		return "metry.gen_ai.measurement.cache_read_input_tokens"
	case UsageReasoningOutputTokens:
		return "metry.gen_ai.measurement.reasoning_output_tokens"
	case UsageCost:
		return "metry.gen_ai.measurement.cost"
	case AudioSeconds:
		return "metry.gen_ai.measurement.audio_seconds"
	case ImageCount:
		return "metry.gen_ai.measurement.image_count"
	case UsageVideoSeconds:
		return "metry.gen_ai.measurement.video_seconds"
	case UsageVideoFrames:
		return "metry.gen_ai.measurement.video_frames"
	default:
		return "metry.gen_ai.measurement.unknown"
	}
}
