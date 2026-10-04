package genai

import (
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	internalgenai "github.com/skosovsky/metry/internal/genai"
)

// NewHintSampler applies keep hints, then parent inheritance, then the base sampler.
// This composition-root API never enables tail sampling.
func NewHintSampler(base sdktrace.Sampler) sdktrace.Sampler {
	return internalgenai.NewHintSamplerSDK(base)
}
