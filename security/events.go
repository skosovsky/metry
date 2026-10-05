package security

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/diagnostic"
	"github.com/skosovsky/metry/internal/traceutil"
)

// Decision is the action already taken by the host. Recording never executes it.
type Decision string

// Observation describes a host decision, not a validator invocation.
// Validator is a required machine identifier; Code and Severity are optional
// machine identifiers. Identifiers contain at most 128 ASCII letters, digits,
// underscores, periods or hyphens. Reason follows the provider diagnostic policy.
type Observation struct {
	Action    Decision
	Validator string
	Reason    string
	Code      string
	Severity  string
	Shadow    bool
}

// ErrMetadata reports invalid bounded machine metadata without echoing its value.
var ErrMetadata = errors.New("security: invalid observation metadata")

// Record observes a host decision on the active recording span, or a new child
// span. Sampling applies normally; this is not a guaranteed audit delivery path.
// A sanitation failure suppresses reason content and never changes the decision.
func Record(ctx context.Context, provider *metry.Provider, observation Observation) error {
	if provider == nil {
		return metry.ErrNilProvider
	}
	if !validObservation(observation) {
		return ErrMetadata
	}
	reason, err := provider.DiagnosticPolicy().Text(diagnostic.ChannelSecurityReason, observation.Reason)
	if errors.Is(err, diagnostic.ErrInput) {
		return err
	}
	if err != nil {
		reason = ""
	}
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		addSecurityEvent(span, observation, reason, err != nil)
		return nil
	}
	suppressed := err != nil
	ctx, end, err := provider.StartSpan(ctx, "metry/security", "security.intervention")
	if err != nil {
		return err
	}
	defer end()
	addSecurityEvent(trace.SpanFromContext(ctx), observation, reason, suppressed)
	return nil
}

func validObservation(observation Observation) bool {
	switch observation.Action {
	case ActionPass, ActionBlock, ActionRedact:
	default:
		return false
	}
	return machineLabel(observation.Validator, true) && machineLabel(observation.Code, false) &&
		machineLabel(observation.Severity, false)
}

func machineLabel(value string, required bool) bool {
	if len(value) > 128 || (required && value == "") {
		return false
	}
	for _, c := range value {
		valid := ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-')
		if !valid {
			return false
		}
	}
	return true
}

func addSecurityEvent(span trace.Span, observation Observation, reason string, suppressed bool) {
	attrs := []attribute.KeyValue{
		attribute.String(
			Action,
			string(observation.Action),
		),
		attribute.String(Validator, observation.Validator),
		attribute.Bool(ShadowMode, observation.Shadow),
	}
	if suppressed {
		attrs = append(attrs, attribute.String("metry.diagnostic.status", "suppressed"))
	}
	if reason != "" {
		attrs = append(attrs, attribute.String(Reason, reason))
	}
	if observation.Code != "" {
		attrs = append(attrs, attribute.String(Code, observation.Code))
	}
	if observation.Severity != "" {
		attrs = append(attrs, attribute.String(Severity, observation.Severity))
	}
	traceutil.MutateRecordingSpan(
		span,
		func(s trace.Span) { s.AddEvent("Security Intervention", trace.WithAttributes(attrs...)) },
	)
}
