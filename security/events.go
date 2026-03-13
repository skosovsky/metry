package security

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// RecordSecurityEvent adds a "Security Intervention" event to the span from ctx
// with action, validator, reason and shadow-mode attributes. Use for compliance
// and audit logging. If ctx has no span, the call is a no-op.
func RecordSecurityEvent(ctx context.Context, action, validator, reason string, isShadow bool) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent("Security Intervention", trace.WithAttributes(
		ActionKey.String(action),
		ValidatorKey.String(validator),
		ReasonKey.String(reason),
		ShadowModeKey.Bool(isShadow),
	))
}
