// Package security provides semantic conventions and helpers for AI security
// observability telemetry.
package security

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

// Standard values for Action.
const (
	ActionPass   = "pass"
	ActionBlock  = "block"
	ActionRedact = "redact"
)
