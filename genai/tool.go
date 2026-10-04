package genai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"
)

const unknownToolName = "unknown"

type toolSpanStateKey struct{}

type toolSpanState struct {
	mu        sync.Mutex
	endOnce   sync.Once
	toolName  string
	startedAt time.Time
	status    string
	errorType string
}

func (t *Tracker) startTool(
	ctx context.Context,
	call ToolCall,
	startOpts ...ToolOption,
) (context.Context, ToolEnd) {
	toolName := toolNameWithConfig(call.Name, t.cfg)
	call.Name = toolName

	attrs := []attribute.KeyValue{
		attribute.Key(OperationName).String(profileToolOperation),
		attribute.Key(ToolName).String(toolName),
		attribute.Key(ToolCallID).String(referenceWithConfig(call.CallID, t.cfg)),
	}
	var payloadErr error
	if t.cfg.RecordPayloads() && call.Arguments != "" {
		payloadErr = validateToolJSONInput(call.Arguments)
		if payloadErr == nil {
			call = t.cfg.PayloadPolicy().SanitizeToolCall(call)
			var encoded string
			encoded, payloadErr = boundedToolJSON(call.Arguments, t.cfg)
			if encoded != "" {
				attrs = append(attrs, attribute.String(ToolCallArguments, encoded))
			}
		}
	}

	otelOpts := childSpanOptionsToOTel(startOpts...)
	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	opts = append(opts, otelOpts...)
	ctx, span := t.tracer.Start(ctx, "tool: "+toolName, opts...)
	span.SetAttributes(attrs...)
	if payloadErr != nil {
		recordPayloadDiagnostic(span, payloadErr)
	}

	state := &toolSpanState{
		mu:        sync.Mutex{},
		endOnce:   sync.Once{},
		toolName:  toolName,
		startedAt: time.Now(),
		status:    OperationStatusOK,
		errorType: "",
	}
	ctx = context.WithValue(ctx, toolSpanStateKey{}, state)

	return ctx, func() {
		state.endOnce.Do(func() {
			status, errorType, elapsed := state.snapshot()
			t.recordToolDuration(ctx, toolName, elapsed, status, errorType)
			traceutil.EndSpanOKIfUnset(span)
		})
	}
}

func (t *Tracker) recordToolResult(ctx context.Context, result ToolResult) {
	status, errorType := t.spanStatusFromToolResult(result)
	if state, ok := ctx.Value(toolSpanStateKey{}).(*toolSpanState); ok && state != nil {
		state.setResult(status, errorType)
	}

	mutateSpan(ctx, func(span trace.Span) {
		t.recordToolPayloadResult(span, result)
		if status == OperationStatusError {
			span.SetAttributes(
				attribute.Key(ToolError).Bool(true),
				attribute.Key(ErrorType).String(errorType),
			)
			traceutil.SpanError(span, errors.New(errorType))
			return
		}
		span.SetAttributes(attribute.Key(ToolError).Bool(false))
		traceutil.SpanOK(span)
	})
}

func (t *Tracker) recordToolPayloadResult(span trace.Span, result ToolResult) {
	if !t.cfg.RecordPayloads() || result.Result == "" {
		return
	}
	if err := validateToolJSONInput(result.Result); err != nil {
		recordPayloadDiagnostic(span, err)
		return
	}
	result = t.cfg.PayloadPolicy().SanitizeToolResult(result)
	encoded, err := boundedToolJSON(result.Result, t.cfg)
	if err != nil {
		recordPayloadDiagnostic(span, err)
		return
	}
	if encoded != "" {
		span.SetAttributes(attribute.String(ToolCallResult, encoded))
	}
}

func (s *toolSpanState) setResult(status, errorType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.errorType = errorType
}

func (s *toolSpanState) snapshot() (string, string, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	if status == "" {
		status = OperationStatusOK
	}
	return status, s.errorType, time.Since(s.startedAt)
}

func (t *Tracker) recordToolDuration(
	ctx context.Context,
	toolName string,
	elapsed time.Duration,
	status string,
	errorType string,
) {
	if t.metrics == nil || t.metrics.ToolDuration == nil || elapsed <= 0 {
		return
	}
	if status == "" {
		status = OperationStatusOK
	}
	attrs := []attribute.KeyValue{
		attribute.String(ToolMetricLabelTool, normalizeToolName(toolName)),
		attribute.String(ToolMetricLabelStatus, status),
	}
	if errorType != "" {
		attrs = append(attrs, attribute.String(ToolMetricLabelErrorType, errorType))
	}
	t.metrics.ToolDuration.Record(
		ctx,
		elapsed.Seconds(),
		metric.WithAttributes(t.boundedMetricAttributes(ToolDurationMetricName, attrs)...),
	)
}

func normalizeToolName(name string) string {
	return sanitizeMetricValue(name, unknownToolName)
}

func (t *Tracker) spanStatusFromToolResult(result ToolResult) (string, string) {
	status := strings.ToLower(strings.TrimSpace(result.Status))
	class := t.classifyToolError(result.Err, result.ErrorClass)
	if result.Err != nil || class != "" || status == OperationStatusError {
		if class == "" {
			class = ErrorClassUnknown
		}
		return OperationStatusError, errorClassString(class)
	}
	if status == "" {
		return OperationStatusOK, ""
	}
	return normalizeOperationStatus(status, ""), ""
}

func (t *Tracker) classifyToolError(err error, explicit ErrorClass) ErrorClass {
	if t == nil {
		return ErrorClassUnknown
	}
	if explicit != "" {
		return t.cfg.NormalizeToolErrorClass(explicit)
	}
	if err == nil {
		return ""
	}
	return t.cfg.NormalizeToolErrorClass(t.cfg.ToolErrorClassifier().ClassifyToolError(err))
}
