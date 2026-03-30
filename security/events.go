package security

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RecordSecurityEvent adds a "Security Intervention" event to the span from ctx
// with action, validator, reason and shadow mode attributes. The code and
// severity attributes are added only when the corresponding argument is not an
// empty string. Use for compliance and audit logging. If ctx has no span, the
// call is a no-op.
func RecordSecurityEvent(ctx context.Context, action, validator, reason, code, severity string, isShadow bool) {
	span := trace.SpanFromContext(ctx)

	attrs := []attribute.KeyValue{
		ActionKey.String(action),
		ValidatorKey.String(validator),
		ReasonKey.String(reason),
		ShadowModeKey.Bool(isShadow),
	}
	if code != "" {
		attrs = append(attrs, CodeKey.String(code))
	}
	if severity != "" {
		attrs = append(attrs, SeverityKey.String(severity))
	}

	span.AddEvent("Security Intervention", trace.WithAttributes(attrs...))
}
