package genai

import (
	"context"
	"strings"
	"time"
	"unicode"

	"go.opentelemetry.io/otel/attribute"

	"github.com/skosovsky/metry"
)

const (
	OperationStatusOK    = "ok"
	OperationStatusError = "error"
	OperationStatusUnset = "unset"
	maxMetricValueLength = 80
)

// Operation describes one generic GenAI operation.
type Operation struct {
	Provider string
	Name     string
	Model    string
	Purpose  string
}

// OperationResult describes the final outcome of one GenAI operation.
type OperationResult struct {
	Status    string
	ErrorType string
	Duration  time.Duration
	Usage     Usage
	Payload   Payload
}

// ToolCall describes one tool execution.
type ToolCall struct {
	Name      string
	CallID    string
	Arguments string
}

// ToolResult describes one tool execution result.
type ToolResult struct {
	Result    string
	Err       error
	ErrorType string
	Status    string
}

// ToolEnd ends a tool span.
type ToolEnd func()

// ToolOption configures tool span start behavior.
type ToolOption = ChildSpanOption

// Recorder is the app-safe GenAI instrumentation boundary.
type Recorder interface {
	RecordOperation(ctx context.Context, op Operation, result OperationResult, attrs ...metry.Attribute) error
	StartTool(ctx context.Context, call ToolCall, opts ...ToolOption) (context.Context, ToolEnd)
	RecordToolResult(ctx context.Context, result ToolResult)
}

type trackerRecorder struct {
	tracker *Tracker
}

type noopRecorder struct{}

// NoopRecorder returns a safe recorder that drops all observations.
func NoopRecorder() Recorder {
	return noopRecorder{}
}

// Recorder returns an app-safe recorder backed by this tracker.
func (t *Tracker) Recorder() Recorder {
	if t == nil {
		return NoopRecorder()
	}
	return trackerRecorder{tracker: t}
}

// NewRecorderFromProvider creates a recorder backed by provider.
func NewRecorderFromProvider(p *metry.Provider, opts ...Option) (Recorder, error) {
	tracker, err := NewTrackerFromProvider(p, opts...)
	if err != nil {
		return nil, err
	}
	return tracker.Recorder(), nil
}

// RecorderFromProvider creates a recorder, returning a no-op recorder when provider setup is incomplete.
func RecorderFromProvider(p *metry.Provider, opts ...Option) Recorder {
	rec, err := NewRecorderFromProvider(p, opts...)
	if err != nil {
		return NoopRecorder()
	}
	return rec
}

func (noopRecorder) RecordOperation(context.Context, Operation, OperationResult, ...metry.Attribute) error {
	return nil
}

func (noopRecorder) StartTool(ctx context.Context, _ ToolCall, _ ...ToolOption) (context.Context, ToolEnd) {
	return ctx, func() {}
}

func (noopRecorder) RecordToolResult(context.Context, ToolResult) {}

func (r trackerRecorder) RecordOperation(
	ctx context.Context,
	op Operation,
	result OperationResult,
	attrs ...metry.Attribute,
) error {
	if r.tracker == nil {
		return nil
	}

	status := normalizeOperationStatus(result.Status, result.ErrorType)
	meta := Meta{ //nolint:exhaustruct // only operation-level fields are relevant here.
		Provider:      op.Provider,
		Operation:     op.Name,
		Purpose:       op.Purpose,
		RequestModel:  op.Model,
		ResponseModel: op.Model,
		Duration:      result.Duration,
		ErrorType:     normalizeOperationErrorType(status, result.ErrorType),
	}
	if result.Usage.Purpose == "" && op.Purpose != "" {
		result.Usage.Purpose = op.Purpose
	}

	scope, hasScope := ScopeFromContext(ctx)
	meta = mergeMetaFromScopeWithScope(meta, scope, hasScope)
	spanName := interactionSpanNameFromMeta(meta, scope, hasScope)
	ctx, span := r.tracker.tracer.Start(ctx, spanName)
	defer span.End()

	if extraAttrs := metryAttributesToOTel(attrs); len(extraAttrs) > 0 {
		span.SetAttributes(extraAttrs...)
	}
	span.SetAttributes(attribute.String(OperationStatus, status))
	r.tracker.recordInteractionOnSpan(ctx, span, meta, result.Payload, result.Usage)
	return nil
}

func (r trackerRecorder) StartTool(ctx context.Context, call ToolCall, opts ...ToolOption) (context.Context, ToolEnd) {
	if r.tracker == nil {
		return ctx, func() {}
	}
	return r.tracker.startTool(ctx, call, opts...)
}

func (r trackerRecorder) RecordToolResult(ctx context.Context, result ToolResult) {
	if r.tracker == nil {
		return
	}
	r.tracker.recordToolResult(ctx, result)
}

func normalizeOperationStatus(status, errorType string) string {
	if errorType != "" {
		return OperationStatusError
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", OperationStatusOK:
		return OperationStatusOK
	case OperationStatusError:
		return OperationStatusError
	case OperationStatusUnset:
		return OperationStatusUnset
	default:
		return OperationStatusUnset
	}
}

func normalizeOperationErrorType(status, errorType string) string {
	if errorType != "" {
		return normalizeErrorType(errorType)
	}
	if status == OperationStatusError {
		return OperationStatusError
	}
	return ""
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

func spanStatusFromToolResult(result ToolResult) (string, string) {
	status := strings.ToLower(strings.TrimSpace(result.Status))
	errorType := result.ErrorType
	if result.Err != nil && errorType == "" {
		errorType = OperationStatusError
	}
	if result.Err != nil || errorType != "" || status == OperationStatusError {
		return OperationStatusError, normalizeErrorType(errorType)
	}
	if status == "" {
		return OperationStatusOK, ""
	}
	return normalizeOperationStatus(status, ""), ""
}

func normalizeErrorType(errorType string) string {
	switch sanitizeMetricValue(strings.ToLower(errorType), "") {
	case "timeout",
		"deadline_exceeded",
		"rate_limit",
		"quota_exceeded",
		"validation",
		"canceled",
		"unavailable",
		"permission_denied",
		"unauthenticated",
		"not_found",
		"conflict",
		"internal",
		OperationStatusError:
		return sanitizeMetricValue(strings.ToLower(errorType), OperationStatusError)
	default:
		return OperationStatusError
	}
}
