package genai

import "go.opentelemetry.io/otel/trace"

// ExecutionLocality describes the observed execution boundary, independently
// from logical/attempt resource accounting identity. Its zero value is invalid
// for interval starts; completed observations always use INTERNAL spans.
type ExecutionLocality string

const (
	ExecutionLocal  ExecutionLocality = "local"
	ExecutionRemote ExecutionLocality = "remote"
)

// Validate rejects unspecified or unsupported boundaries without inference.
func (l ExecutionLocality) Validate() error {
	if l != ExecutionLocal && l != ExecutionRemote {
		return invalidObservation("locality", "invalid_enum")
	}
	return nil
}

func (l ExecutionLocality) spanKind() trace.SpanKind {
	if l == ExecutionRemote {
		return trace.SpanKindClient
	}
	return trace.SpanKindInternal
}
