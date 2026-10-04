package genai

import (
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Outcome distinguishes a known result from an observation without a result.
// Its zero value is unknown. Producers must explicitly report success.
type Outcome string

func resolveOutcome(status Outcome, hasError bool) (string, error) {
	switch status {
	case "":
		if hasError {
			return OperationStatusError, nil
		}
		return OperationStatusUnset, nil
	case OperationStatusOK, OperationStatusUnset:
		if hasError {
			return "", invalidObservation("outcome", "conflicting")
		}
		return string(status), nil
	case OperationStatusError:
		return OperationStatusError, nil
	default:
		return "", invalidObservation("outcome", "invalid_enum")
	}
}

func resolveModelOutcome(status Outcome, errorType string) (string, string, error) {
	if errorType != "" && !validModelErrorType(errorType) {
		return "", "", invalidObservation("error_class", "invalid_enum")
	}
	resolved, err := resolveOutcome(status, errorType != "")
	if err != nil {
		return "", "", err
	}
	if resolved == OperationStatusError && errorType == "" {
		errorType = string(ErrorClassUnknown)
	}
	return resolved, errorType, nil
}

func validModelErrorType(value string) bool {
	switch value {
	case string(ErrorClassUnknown), OperationStatusError, string(ErrorClassCancelled),
		string(ErrorClassDeadlineExceeded), string(ErrorClassStreamAborted),
		"timeout", "rate_limit", "quota_exceeded", "validation", "unavailable",
		"permission_denied", "unauthenticated", "not_found", "conflict", "internal":
		return true
	default:
		return false
	}
}

func resolveToolOutcome(t *Tracker, status Outcome, businessErr error, explicit ErrorClass) (string, string, error) {
	cfg := defaultRuntimeConfig()
	if t != nil {
		cfg = t.cfg
	}
	if explicit != "" {
		if _, ok := cfg.allowedToolErrorClasses[explicit]; !ok {
			return "", "", invalidObservation("error_class", "invalid_enum")
		}
	}
	resolved, err := resolveOutcome(status, businessErr != nil || explicit != "")
	if err != nil {
		return "", "", err
	}
	if resolved != OperationStatusError {
		return resolved, "", nil
	}
	class := explicit
	if class == "" && businessErr != nil {
		class = cfg.NormalizeToolErrorClass(cfg.ToolErrorClassifier().ClassifyToolError(businessErr))
	}
	if class == "" {
		class = ErrorClassUnknown
	}
	return resolved, string(class), nil
}

func applyOutcome(span trace.Span, status, errorType string) {
	span.SetAttributes(attribute.String(OperationStatus, status))
	if errorType != "" {
		span.SetAttributes(attribute.String(ErrorType, errorType))
	}
	switch status {
	case OperationStatusOK:
		span.SetStatus(codes.Ok, "")
	case OperationStatusError:
		span.RecordError(errors.New(errorType))
		span.SetStatus(codes.Error, errorType)
	default:
		span.SetStatus(codes.Unset, "")
	}
}
