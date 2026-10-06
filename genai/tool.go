package genai

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const unknownToolName = "unknown"

// ToolHandle owns one local tool observation, never the tool execution.
type ToolHandle struct {
	mu        sync.Mutex
	finished  bool
	tracker   *Tracker
	ctx       context.Context
	span      trace.Span
	toolName  string
	startedAt time.Time
}

func startTool(
	ctx context.Context,
	t *Tracker,
	call ToolCall,
	startOpts ...ToolOption,
) (context.Context, *ToolHandle, error) {
	if len(call.Name) > maxIdentityBytes || len(call.CallID) > maxIdentityBytes {
		return ctx, nil, invalidObservation("tool", "input_bounds")
	}
	state := &ToolHandle{
		mu:        sync.Mutex{},
		finished:  false,
		tracker:   t,
		ctx:       ctx,
		span:      nil,
		toolName:  "",
		startedAt: time.Now(),
	}
	if t == nil {
		return ctx, state, nil
	}
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
	opts := []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
		trace.WithTimestamp(state.startedAt),
		trace.WithSpanKind(trace.SpanKindInternal),
	}
	opts = append(opts, childSpanOptionsToOTel(startOpts...)...)
	ctx, span := t.tracer.Start(ctx, "execute_tool "+toolName, opts...)
	span.SetAttributes(attrs...)
	if payloadErr != nil {
		recordPayloadDiagnostic(span, payloadErr)
	}
	state.ctx, state.span, state.toolName = ctx, span, toolName
	return ctx, state, nil
}

// Finish commits a validated result exactly once. Invalid input is correctable.
func (h *ToolHandle) Finish(result ToolResult) error {
	if h == nil || h.startedAt.IsZero() {
		return invalidObservation("recording", "missing")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return ErrFinalized
	}
	defer endRecordingOnPanic(h.span, &h.finished)
	status, errorType, err := resolveToolOutcome(h.tracker, result.Status, result.Err, result.ErrorClass)
	if err != nil {
		return err
	}
	h.finished = true
	if h.tracker != nil {
		h.tracker.recordToolPayloadResult(h.span, result)
		h.span.SetAttributes(attribute.Bool(ToolError, status == OperationStatusError))
		applyOutcome(h.span, status, errorType)
		h.tracker.recordToolDuration(h.ctx, h.toolName, time.Since(h.startedAt), status, errorType)
		h.span.End()
	}
	return nil
}

// Close closes incomplete telemetry with unknown outcome and no fabricated result.
func (h *ToolHandle) Close() error {
	return h.Finish(ToolResult{Status: OperationStatusUnset, Result: "", Err: nil, ErrorClass: ""})
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
		status = OperationStatusUnset
	}
	attrs := []attribute.KeyValue{
		attribute.String(ToolName, normalizeToolName(toolName)),
		attribute.String(OperationStatus, status),
	}
	if errorType != "" {
		attrs = append(attrs, attribute.String(ErrorType, errorType))
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
