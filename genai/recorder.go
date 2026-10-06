package genai

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
)

const (
	OperationStatusOK    = "ok"
	OperationStatusError = "error"
	OperationStatusUnset = "unset"
	maxMetricValueLength = 80
	asyncResultAttrCap   = 3
	unknownProviderName  = "unknown"
)

// Operation describes one generic GenAI operation.
type Operation struct {
	Name     string
	Model    string
	Purpose  string
	Locality ExecutionLocality
}

// OperationResult describes the final outcome of one GenAI operation.
type OperationResult struct {
	Status    Outcome
	Response  ResponseMetadata
	ErrorType string
	Duration  time.Duration
	Usage     Usage
	Payload   Payload
	Streaming *StreamTimings
}

// StreamTimings describes first-token timing measured by the producer from this
// operation's start. Unknown/aborted streams leave FirstTokenPresent false.
type StreamTimings struct {
	TTFT              time.Duration
	FirstTokenPresent bool
}

// ToolCall describes one tool execution.
type ToolCall struct {
	Name      string
	CallID    string
	Arguments string
}

// ToolResult describes one tool execution result.
type ToolResult struct {
	Result     string
	Err        error
	ErrorClass ErrorClass
	Status     Outcome
}

// ToolOption configures tool span start behavior.
type ToolOption = ChildSpanOption

// StreamingCompletion describes one completed streaming response.
type StreamingCompletion struct {
	Meta              Meta
	Usage             Usage
	TTFT              time.Duration
	TotalDuration     time.Duration
	FirstTokenPresent bool
}

// AsyncResult describes a deferred GenAI outcome linked to an AsyncHandle.
type AsyncResult struct {
	Name       string
	Status     string
	Err        error
	ErrorClass ErrorClass
	Attributes []metry.Attribute
}

// AsyncOption configures async handle capture.
type AsyncOption func(*asyncStartConfig)

type asyncStartConfig struct {
	attrs []metry.Attribute
}

// WithAsyncAttributes adds typed attributes to the async origin span.
func WithAsyncAttributes(attrs ...metry.Attribute) AsyncOption {
	return func(c *asyncStartConfig) {
		c.attrs = append(c.attrs, attrs...)
	}
}

// Recorder is the app-scoped GenAI instrumentation boundary.
type Recorder interface {
	StartAgent(context.Context, AgentInvocation, RecordingOptions) (context.Context, *InvocationHandle, error)
	StartWorkflow(context.Context, WorkflowInvocation, RecordingOptions) (context.Context, *InvocationHandle, error)
	StartOperation(
		ctx context.Context,
		op Operation,
		options RecordingOptions,
	) (context.Context, *OperationHandle, error)
	ForProvider(provider string) Recorder
	RecordOperation(ctx context.Context, op Operation, result OperationResult, attrs ...metry.Attribute) error
	RecordTraceIO(ctx context.Context, input, output Payload) error
	RecordStreamingCompletion(ctx context.Context, event StreamingCompletion) error
	StartTool(ctx context.Context, call ToolCall, opts ...ToolOption) (context.Context, *ToolHandle, error)
	StartRetrieval(
		ctx context.Context,
		name string,
		req RetrievalRequest,
		opts ...ChildSpanOption,
	) (context.Context, *RetrievalHandle, error)
	StartAsync(ctx context.Context, name string, opts ...AsyncOption) (metry.AsyncHandle, error)
	RecordAsyncResult(ctx context.Context, handle metry.AsyncHandle, result AsyncResult) error
	RecordAsyncTokenResult(ctx context.Context, token string, result AsyncResult) error
}

type trackerRecorder struct {
	tracker  *Tracker
	provider string
}

type noopRecorder struct{}

// NoopRecorder returns a safe recorder that drops all observations.
func NoopRecorder() Recorder {
	return noopRecorder{}
}

// Recorder returns an app-scoped recorder backed by this tracker.
func (t *Tracker) Recorder() Recorder {
	if t == nil {
		return NoopRecorder()
	}
	return trackerRecorder{tracker: t, provider: ""}
}

// NewRecorderFromProvider creates a recorder backed by provider.
// Configuration and instrument creation errors are returned to the caller.
// Optional telemetry must select NoopRecorder explicitly at the composition root.
func NewRecorderFromProvider(p *metry.Provider, opts ...Option) (Recorder, error) {
	tracker, err := NewTrackerFromProvider(p, opts...)
	if err != nil {
		return nil, err
	}
	return tracker.Recorder(), nil
}

func (noopRecorder) ForProvider(string) Recorder {
	return noopRecorder{}
}

func (noopRecorder) RecordOperation(
	ctx context.Context,
	operation Operation,
	result OperationResult,
	_ ...metry.Attribute,
) error {
	if err := validateModelMetadata(operation.Model, operation.Purpose); err != nil {
		return err
	}
	meta := emptyMeta()
	meta.Operation = operation.Name
	meta.RequestModel = operation.Model
	meta.Purpose = operation.Purpose
	meta = mergeMetaFromScope(ctx, meta)
	if err := validateOperationName(meta.Operation); err != nil {
		return err
	}
	if err := validateModelMetadata(meta.RequestModel, meta.Purpose); err != nil {
		return err
	}
	return validateCompletedResult(result)
}

func validateOperationName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > maxIdentityBytes || !utf8.ValidString(name) {
		return invalidObservation("operation", "malformed")
	}
	return nil
}

func (noopRecorder) RecordTraceIO(context.Context, Payload, Payload) error {
	return nil
}

func (noopRecorder) RecordStreamingCompletion(ctx context.Context, event StreamingCompletion) error {
	if _, _, err := resolveModelOutcome(event.Meta.Status, event.Meta.ErrorType); err != nil {
		return err
	}
	meta := mergeMetaFromScope(ctx, event.Meta)
	if err := validateOperationName(meta.Operation); err != nil {
		return err
	}
	if err := validateModelMetadata(meta.RequestModel, meta.Purpose); err != nil {
		return err
	}
	if err := validateModelMetadata(meta.ResponseModel, ""); err != nil {
		return err
	}
	if err := event.Usage.Validate(); err != nil {
		return err
	}
	return validateStreamTimings(
		StreamTimings{TTFT: event.TTFT, FirstTokenPresent: event.FirstTokenPresent},
		event.TotalDuration,
	)
}

func (noopRecorder) StartTool(
	ctx context.Context,
	call ToolCall,
	opts ...ToolOption,
) (context.Context, *ToolHandle, error) {
	return startTool(ctx, nil, call, opts...)
}

func (noopRecorder) StartRetrieval(
	ctx context.Context,
	name string,
	req RetrievalRequest,
	opts ...ChildSpanOption,
) (context.Context, *RetrievalHandle, error) {
	return startRetrieval(ctx, nil, name, req, opts...)
}

func (noopRecorder) StartAsync(context.Context, string, ...AsyncOption) (metry.AsyncHandle, error) {
	return metry.NoopAsyncHandle(), nil
}

func (noopRecorder) RecordAsyncResult(_ context.Context, handle metry.AsyncHandle, _ AsyncResult) error {
	if !handle.IsValid() {
		return ErrInvalidAsyncHandle
	}
	return nil
}

func (noopRecorder) RecordAsyncTokenResult(_ context.Context, token string, _ AsyncResult) error {
	if _, err := metry.ParseAsyncHandle(token); err != nil {
		return err
	}
	return nil
}

func (r trackerRecorder) ForProvider(provider string) Recorder {
	if r.tracker == nil {
		return NoopRecorder()
	}
	r.provider = strings.TrimSpace(provider)
	return r
}

func (r trackerRecorder) RecordOperation(
	ctx context.Context,
	op Operation,
	result OperationResult,
	attrs ...metry.Attribute,
) error {
	if r.tracker == nil {
		return NoopRecorder().RecordOperation(ctx, op, result)
	}
	if err := validateModelMetadata(op.Model, op.Purpose); err != nil {
		return err
	}
	if err := validateCompletedResult(result); err != nil {
		return err
	}
	if err := r.tracker.validateUsage(result.Usage); err != nil {
		return err
	}
	status, class, outcomeErr := resolveModelOutcome(result.Status, result.ErrorType)
	if outcomeErr != nil {
		return outcomeErr
	}
	meta := Meta{ //nolint:exhaustruct_v5 // only operation-level fields are relevant here.
		Provider:      r.recorderProvider(),
		Operation:     op.Name,
		Purpose:       op.Purpose,
		RequestModel:  op.Model,
		ResponseModel: result.Response.Model,
		Duration:      result.Duration,
		ErrorType:     class,
		Status:        Outcome(status),
	}
	if result.Usage.Purpose == "" && op.Purpose != "" {
		result.Usage.Purpose = op.Purpose
	}

	meta = mergeMetaFromScope(ctx, meta)
	if err := validateOperationName(meta.Operation); err != nil {
		return err
	}
	if err := validateModelMetadata(meta.RequestModel, meta.Purpose); err != nil {
		return err
	}
	payloadAttrs, err := buildPayloadAttributes(result.Payload, r.tracker.cfg)
	if err != nil {
		return err
	}
	ctx, span := r.tracker.tracer.Start(
		ctx,
		modelSpanName(meta, r.tracker.cfg),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	if extraAttrs := metryAttributesToOTel(attrs); len(extraAttrs) > 0 {
		span.SetAttributes(extraAttrs...)
	}
	span.SetAttributes(attribute.String(OperationStatus, status))
	span.SetAttributes(attribute.String("metry.observation.kind", "completed"))
	r.tracker.recordInteractionOnSpan(ctx, span, meta, payloadAttrs, result.Usage, true)
	if result.Streaming != nil {
		r.tracker.recordStreamingQuality(ctx, meta, result.Usage.OutputTokens, *result.Streaming, result.Duration)
	}
	return nil
}

func validateCompletedResult(result OperationResult) error {
	if err := validateModelMetadata(result.Response.Model, ""); err != nil {
		return err
	}
	if _, _, err := resolveModelOutcome(result.Status, result.ErrorType); err != nil {
		return err
	}
	if err := result.Usage.Validate(); err != nil {
		return err
	}
	if result.Duration < 0 {
		return invalidObservation("duration", "invalid_interval")
	}
	if result.Streaming != nil {
		return validateStreamTimings(*result.Streaming, result.Duration)
	}
	return nil
}

func (r trackerRecorder) RecordTraceIO(ctx context.Context, input, output Payload) error {
	if r.tracker == nil {
		return nil
	}
	if err := r.tracker.RecordTraceIO(ctx, input, output); err != nil {
		return err
	}
	mutateSpan(ctx, func(span trace.Span) {
		span.SetAttributes(
			attribute.Key(ProviderName).String(metadataWithConfig(ProviderName, r.recorderProvider(), r.tracker.cfg)),
		)
	})
	return nil
}

func (r trackerRecorder) RecordStreamingCompletion(ctx context.Context, event StreamingCompletion) error {
	if r.tracker == nil {
		return NoopRecorder().RecordStreamingCompletion(ctx, event)
	}
	event.Meta = r.bindMeta(event.Meta)
	return r.tracker.RecordStreamingCompletion(ctx, event)
}

func (r trackerRecorder) StartTool(
	ctx context.Context,
	call ToolCall,
	opts ...ToolOption,
) (context.Context, *ToolHandle, error) {
	if r.tracker == nil {
		return startTool(ctx, nil, call, opts...)
	}
	options := append([]ToolOption(nil), opts...)
	options = append(
		options,
		WithSpanAttributes(
			metry.StringAttribute(ProviderName, metadataWithConfig(ProviderName, r.recorderProvider(), r.tracker.cfg)),
		),
	)
	return startTool(ctx, r.tracker, call, options...)
}

func (r trackerRecorder) StartRetrieval(
	ctx context.Context,
	name string,
	req RetrievalRequest,
	opts ...ChildSpanOption,
) (context.Context, *RetrievalHandle, error) {
	return startRetrieval(ctx, r.tracker, name, req, opts...)
}

func (r trackerRecorder) StartAsync(
	ctx context.Context,
	name string,
	opts ...AsyncOption,
) (metry.AsyncHandle, error) {
	if r.tracker == nil {
		return metry.AsyncHandle{}, nil
	}

	spanName := strings.TrimSpace(name)
	if spanName == "" {
		spanName = "genai.async"
	}
	spanName = metadataWithConfig(OperationName, spanName, r.tracker.cfg)
	cfg := buildAsyncStartConfig(opts...)
	attrs := metryAttributesToOTel(cfg.attrs)
	attrs = append(attrs, attribute.Key(OperationName).String(spanName))
	attrs = append(
		attrs,
		attribute.Key(ProviderName).String(metadataWithConfig(ProviderName, r.recorderProvider(), r.tracker.cfg)),
	)

	ctx, span := r.tracker.tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
	defer span.End()
	return metry.NewAsyncHandle(ctx)
}

func (r trackerRecorder) RecordAsyncResult(
	ctx context.Context,
	handle metry.AsyncHandle,
	result AsyncResult,
) error {
	if r.tracker == nil {
		return nil
	}
	if !handle.IsValid() {
		return ErrInvalidAsyncHandle
	}

	status, class := r.asyncStatusAndClass(result)
	attrs := make([]metry.Attribute, 0, len(result.Attributes)+asyncResultAttrCap)
	attrs = append(attrs, filterAsyncResultAttributes(result.Attributes)...)
	attrs = append(attrs, metry.StringAttribute(OperationStatus, status))
	attrs = append(
		attrs,
		metry.StringAttribute(ProviderName, metadataWithConfig(ProviderName, r.recorderProvider(), r.tracker.cfg)),
	)
	if class != "" {
		attrs = append(attrs, metry.StringAttribute(ErrorType, errorClassString(class)))
	}

	spanName := strings.TrimSpace(result.Name)
	if spanName == "" {
		spanName = "genai.async.result"
	}
	spanName = metadataWithConfig(OperationName, spanName, r.tracker.cfg)
	return handle.RecordLinkedSpan(ctx, r.tracker.provider, spanName, func(w metry.LinkedSpanWriter) error {
		w.SetAttributes(attrs...)
		if status == OperationStatusError {
			errText := errorClassString(class)
			if errText == "" {
				errText = OperationStatusError
			}
			w.SetError(errors.New(errText))
		}
		return nil
	})
}

func (r trackerRecorder) RecordAsyncTokenResult(
	ctx context.Context,
	token string,
	result AsyncResult,
) error {
	handle, err := metry.ParseAsyncHandle(token)
	if err != nil {
		return err
	}
	return r.RecordAsyncResult(ctx, handle, result)
}

func (r trackerRecorder) bindMeta(meta Meta) Meta {
	meta.Provider = r.recorderProvider()
	return meta
}

func (r trackerRecorder) recorderProvider() string {
	if r.provider == "" {
		return unknownProviderName
	}
	return r.provider
}

func (r trackerRecorder) asyncStatusAndClass(result AsyncResult) (string, ErrorClass) {
	class := r.tracker.classifyToolError(result.Err, result.ErrorClass)
	status := strings.ToLower(strings.TrimSpace(result.Status))
	if result.Err != nil || class != "" || status == OperationStatusError {
		if class == "" {
			class = ErrorClassUnknown
		}
		return OperationStatusError, class
	}
	if status == "" || status == OperationStatusOK {
		return OperationStatusOK, ""
	}
	if status == OperationStatusUnset {
		return OperationStatusUnset, ""
	}
	return OperationStatusUnset, ""
}

func buildAsyncStartConfig(opts ...AsyncOption) asyncStartConfig {
	var cfg asyncStartConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

func emptyMeta() Meta {
	return Meta{
		Provider:      "",
		Operation:     "",
		Purpose:       "",
		RequestModel:  "",
		ResponseModel: "",
		ServerAddress: "",
		ServerPort:    0,
		Duration:      0,
		ErrorType:     "",
		Status:        "",
		interval:      false,
		locality:      "",
	}
}

func filterAsyncResultAttributes(attrs []metry.Attribute) []metry.Attribute {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]metry.Attribute, 0, len(attrs))
	for _, attr := range attrs {
		switch attr.Key() {
		case ProviderName, OperationStatus, ErrorType:
			continue
		default:
			out = append(out, attr)
		}
	}
	return out
}

func metryAttributesToOTel(attrs []metry.Attribute) []attribute.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		key := attr.Key()
		if key == "" {
			continue
		}
		switch attr.Kind() {
		case metry.AttributeKindFloat:
			if value, ok := attr.Float64Value(); ok {
				out = append(out, attribute.Float64(key, value))
			}
		case metry.AttributeKindBool:
			if value, ok := attr.BoolValue(); ok {
				out = append(out, attribute.Bool(key, value))
			}
		case metry.AttributeKindInt:
			if value, ok := attr.Int64Value(); ok {
				out = append(out, attribute.Int64(key, value))
			}
		default:
			if value, ok := attr.StringValue(); ok {
				out = append(out, attribute.String(key, value))
			}
		}
	}
	return out
}

func sanitizeMetricValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_', r == '-', r == '.', r == '/':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('_')
		}
		if b.Len() >= maxMetricValueLength {
			break
		}
	}
	if b.Len() == 0 {
		return fallback
	}
	return b.String()
}
