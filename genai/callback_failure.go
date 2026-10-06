package genai

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Deferred while the handle mutex is held, before any host callback. A panic
// terminates the recording even if it interrupted pre-commit validation. The
// host still owns the panic; neither DiagnosticPolicy nor formatting sees it.
func endRecordingOnPanic(span trace.Span, finished *bool) {
	if value := recover(); value != nil {
		*finished = true
		if span != nil {
			// Do not defer span.End directly: the SDK would recover the active
			// panic and append its raw object as another exception event.
			defer func() { span.End() }()
			applyOutcome(span, OperationStatusError, string(ErrorClassUnknown))
			span.AddEvent("metry.observation.rejected", trace.WithAttributes(
				attribute.String("metry.diagnostic.field", "callback"),
				attribute.String("metry.diagnostic.rule", "panic"),
			))
		}
		panic(value)
	}
}
