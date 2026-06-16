package genai

import (
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type hintSampler struct {
	base sdktrace.Sampler
}

// NewHintSamplerSDK wraps a base SDK sampler with explicit keep-hint support.
func NewHintSamplerSDK(base sdktrace.Sampler) sdktrace.Sampler {
	if base == nil {
		base = sdktrace.NeverSample()
	}
	return hintSampler{base: base}
}

func (s hintSampler) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	if hasSamplingKeepHint(parameters.Attributes) {
		return sdktrace.SamplingResult{
			Decision:   sdktrace.RecordAndSample,
			Attributes: nil,
			Tracestate: trace.SpanContextFromContext(parameters.ParentContext).TraceState(),
		}
	}

	parent := trace.SpanContextFromContext(parameters.ParentContext)
	if parent.IsValid() {
		decision := sdktrace.Drop
		if parent.IsSampled() {
			decision = sdktrace.RecordAndSample
		}
		return sdktrace.SamplingResult{
			Decision:   decision,
			Attributes: nil,
			Tracestate: parent.TraceState(),
		}
	}

	return s.base.ShouldSample(parameters)
}

func (s hintSampler) Description() string {
	return "genai.HintSampler{" + s.base.Description() + "}"
}

func hasSamplingKeepHint(attrs []attribute.KeyValue) bool {
	for i := len(attrs) - 1; i >= 0; i-- {
		attrKV := attrs[i]
		if attrKV.Key != attribute.Key("gen_ai.sampling.keep") {
			continue
		}
		return attrKV.Value.Type() == attribute.BOOL && attrKV.Value.AsBool()
	}
	return false
}
