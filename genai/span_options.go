package genai

import (
	"github.com/skosovsky/metry"
)

// LinkedSpanOption configures linked async spans without OpenTelemetry types.
type LinkedSpanOption = metry.LinkedSpanOption

// WithSamplingKeep requests head sampling for a linked async span.
func WithSamplingKeep() LinkedSpanOption {
	return metry.WithLinkedAttributes(metry.BoolAttribute(SamplingKeep, true))
}

// WithLinkedPurpose sets operation purpose on a linked async span.
func WithLinkedPurpose(purpose string) LinkedSpanOption {
	return metry.WithLinkedAttributes(metry.StringAttribute(OperationPurpose, purpose))
}

// WithSpanSamplingKeep requests head sampling when starting a child span.
func WithSpanSamplingKeep() ChildSpanOption {
	return WithSpanAttributes(metry.BoolAttribute(SamplingKeep, true))
}
