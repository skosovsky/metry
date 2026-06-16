package genai

import (
	"fmt"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/genaiwire"
)

// NewHintSampler wraps a base sampler with explicit keep-hint support.
// Sampling order:
// 1) keep hint at span start forces sampling;
// 2) otherwise, valid parent sampling decision is inherited;
// 3) otherwise, base sampler decides.
func NewHintSampler(base metry.TraceSampler) metry.TraceSampler {
	v := genaiwire.NewHintSampler(base)
	if v == nil {
		panic("genai: NewHintSampler wire hook returned nil (init order?)")
	}
	s, ok := v.(metry.TraceSampler)
	if !ok {
		panic(fmt.Sprintf("genai: NewHintSampler wire hook returned %T", v))
	}
	return s
}
