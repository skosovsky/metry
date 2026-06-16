package security

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/traceutil"
)

// RecordSecurityEventWithProvider records a security event using provider.StartSpan when
// ctx has no active span, so hosts do not need direct TracerProvider access.
func RecordSecurityEventWithProvider(
	ctx context.Context,
	provider *metry.Provider,
	action, validator, reason, code, severity string,
	isShadow bool,
) error {
	if provider == nil {
		return metry.ErrNilProvider
	}
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		addSecurityEvent(span, action, validator, reason, code, severity, isShadow)
		return nil
	}
	ctx, end, err := provider.StartSpan(ctx, "metry/security", "security.intervention")
	if err != nil {
		return err
	}
	defer end()
	addSecurityEvent(trace.SpanFromContext(ctx), action, validator, reason, code, severity, isShadow)
	return nil
}

func addSecurityEvent(span trace.Span, action, validator, reason, code, severity string, isShadow bool) {
	attrs := []attribute.KeyValue{
		attribute.String(Action, action),
		attribute.String(Validator, validator),
		attribute.String(Reason, reason),
		attribute.Bool(ShadowMode, isShadow),
	}
	if code != "" {
		attrs = append(attrs, attribute.String(Code, code))
	}
	if severity != "" {
		attrs = append(attrs, attribute.String(Severity, severity))
	}
	traceutil.MutateRecordingSpan(span, func(s trace.Span) {
		s.AddEvent("Security Intervention", trace.WithAttributes(attrs...))
	})
}
