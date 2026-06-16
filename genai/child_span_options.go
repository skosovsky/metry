package genai

import (
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/linkedspan"
)

type childSpanConfig struct {
	attrs []metry.Attribute
}

// ChildSpanOption configures child span start behavior without exposing OTel types.
type ChildSpanOption func(*childSpanConfig)

func childSpanOptionsToLinkedspan(opts ...ChildSpanOption) []linkedspan.Option {
	if len(opts) == 0 {
		return nil
	}
	var cfg childSpanConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if len(cfg.attrs) == 0 {
		return nil
	}
	return []linkedspan.Option{linkedspan.WithAttributes(cfg.attrs...)}
}

func childSpanOptionsToOTel(opts ...ChildSpanOption) []trace.SpanStartOption {
	return linkedspan.OptionsToOTel(childSpanOptionsToLinkedspan(opts...)...)
}

// WithSpanAttributes adds typed metry attributes at child span start.
func WithSpanAttributes(attrs ...metry.Attribute) ChildSpanOption {
	return func(c *childSpanConfig) {
		c.attrs = append(c.attrs, attrs...)
	}
}
