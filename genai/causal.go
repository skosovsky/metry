package genai

import (
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
)

const (
	maxIdentityBytes   = 256
	maxCausalLinks     = 16
	causalRunKey       = "metry.causal.run_id"
	causalParentRunKey = "metry.causal.parent_run_id"
	causalOperationKey = "metry.causal.operation_id"
	causalStepKey      = "metry.causal.step_id"
	causalAttemptKey   = "metry.causal.attempt_id"
	causalKindKey      = "metry.causal.kind"
	causalPromptKey    = "metry.causal.prompt_identity"
)

// IdentityKind distinguishes a logical summary from physical resource accounting.
type IdentityKind string

const (
	IdentityLogical   IdentityKind = "logical"
	IdentityOperation IdentityKind = "operation"
	IdentityAttempt   IdentityKind = "attempt"
)

// CausalIdentity is caller-owned business identity, independent from trace tokens.
// An entirely empty identity disables causal mode. Recorders capture a value copy.
type CausalIdentity struct {
	RunID          string
	ParentRunID    string
	OperationID    string
	StepID         string
	AttemptID      string
	Kind           IdentityKind
	PromptIdentity string
}

// Validate requires explicit business IDs and rejects malformed metadata without
// echoing the value or generating substitute identifiers.
func (i CausalIdentity) Validate() error {
	if i.empty() {
		return nil
	}
	if i.RunID == "" || i.OperationID == "" {
		return invalidObservation("identity", "missing_required")
	}
	if i.Kind != IdentityLogical && i.Kind != IdentityOperation && i.Kind != IdentityAttempt {
		return invalidObservation("identity", "invalid_kind")
	}
	if i.Kind == IdentityAttempt && i.AttemptID == "" {
		return invalidObservation("identity", "missing_attempt")
	}
	if i.Kind != IdentityAttempt && i.AttemptID != "" {
		return invalidObservation("identity", "unexpected_attempt")
	}
	if i.ParentRunID != "" && i.ParentRunID == i.RunID {
		return invalidObservation("identity", "self_parent")
	}
	for _, value := range []string{i.RunID, i.ParentRunID, i.OperationID, i.StepID, i.AttemptID, i.PromptIdentity} {
		if !boundedIdentity(value) {
			return invalidObservation("identity", "malformed")
		}
	}
	return nil
}

func boundedIdentity(value string) bool {
	if len(value) > maxIdentityBytes || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char <= ' ' || char == 127 {
			return false
		}
	}
	return true
}

func (i CausalIdentity) empty() bool {
	return i.RunID == "" && i.ParentRunID == "" && i.OperationID == "" &&
		i.StepID == "" && i.AttemptID == "" && i.Kind == "" && i.PromptIdentity == ""
}

func (i CausalIdentity) attributes(cfg runtimeConfig) []attribute.KeyValue {
	if i.empty() {
		return nil
	}
	attrs := []attribute.KeyValue{
		attribute.String(causalRunKey, referenceWithConfig(i.RunID, cfg)),
		attribute.String(causalOperationKey, referenceWithConfig(i.OperationID, cfg)),
		attribute.String(causalKindKey, string(i.Kind)),
	}
	for _, optional := range []struct{ key, value string }{
		{causalParentRunKey, i.ParentRunID},
		{causalStepKey, i.StepID},
		{causalAttemptKey, i.AttemptID},
		{causalPromptKey, i.PromptIdentity},
	} {
		if optional.value != "" {
			attrs = append(attrs, attribute.String(optional.key, referenceWithConfig(optional.value, cfg)))
		}
	}
	return attrs
}
