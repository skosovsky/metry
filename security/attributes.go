// Package security provides semantic conventions and helpers for AI security
// observability telemetry.
package security

import "go.opentelemetry.io/otel/attribute"

// Security semantic convention attribute keys for traces and events.
const (
	Tier       = "ai.security.tier"
	Validator  = "ai.security.validator"
	Action     = "ai.security.action"
	ShadowMode = "ai.security.shadow_mode"
	Score      = "ai.security.score"
	Reason     = "ai.security.reason"
	Code       = "ai.security.code"
	Severity   = "ai.security.severity"
)

// Attribute keys as attribute.Key for type-safe span/event recording.
var (
	TierKey       = attribute.Key(Tier)
	ValidatorKey  = attribute.Key(Validator)
	ActionKey     = attribute.Key(Action)
	ShadowModeKey = attribute.Key(ShadowMode)
	ScoreKey      = attribute.Key(Score)
	ReasonKey     = attribute.Key(Reason)
	CodeKey       = attribute.Key(Code)
	SeverityKey   = attribute.Key(Severity)
)

// Standard values for Action.
const (
	ActionPass   = "pass"
	ActionBlock  = "block"
	ActionRedact = "redact"
)
