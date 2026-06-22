package genai

import (
	"context"
	"errors"
	"strings"
)

// ErrorClass is the bounded error taxonomy used by GenAI tool telemetry.
type ErrorClass string

const (
	ErrorClassUnknown          ErrorClass = "unknown"
	ErrorClassCancelled        ErrorClass = "cancelled"
	ErrorClassDeadlineExceeded ErrorClass = "deadline_exceeded"
	ErrorClassPaused           ErrorClass = "paused"
	ErrorClassStreamAborted    ErrorClass = "stream_aborted"
)

// ToolErrorClassifier maps tool execution errors onto bounded error classes.
type ToolErrorClassifier interface {
	ClassifyToolError(error) ErrorClass
}

// ToolErrorClassifierFunc adapts a function to ToolErrorClassifier.
type ToolErrorClassifierFunc func(error) ErrorClass

// ClassifyToolError implements ToolErrorClassifier.
func (f ToolErrorClassifierFunc) ClassifyToolError(err error) ErrorClass {
	if f == nil {
		return ErrorClassUnknown
	}
	return f(err)
}

type defaultToolErrorClassifier struct{}

func (defaultToolErrorClassifier) ClassifyToolError(err error) ErrorClass {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return ErrorClassCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorClassDeadlineExceeded
	default:
		return ErrorClassUnknown
	}
}

func defaultAllowedToolErrorClasses() map[ErrorClass]struct{} {
	return map[ErrorClass]struct{}{
		ErrorClassUnknown:          {},
		ErrorClassCancelled:        {},
		ErrorClassDeadlineExceeded: {},
		ErrorClassPaused:           {},
		ErrorClassStreamAborted:    {},
	}
}

func normalizeErrorClass(value ErrorClass, allowed map[ErrorClass]struct{}) ErrorClass {
	value = sanitizeErrorClassLabel(value)
	if value == "" {
		return ""
	}
	if _, ok := allowed[value]; ok {
		return value
	}
	return ErrorClassUnknown
}

func sanitizeErrorClassLabel(value ErrorClass) ErrorClass {
	return ErrorClass(sanitizeMetricValue(strings.ToLower(strings.TrimSpace(string(value))), ""))
}

func errorClassString(value ErrorClass) string {
	if value == "" {
		return ""
	}
	return string(value)
}
