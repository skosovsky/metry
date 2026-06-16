package linkedspan

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/attrconv"
)

// Option configures linked span start behavior without exposing OTel types to callers.
type Option func(*config)

type config struct {
	otelOpts []trace.SpanStartOption
}

// WithAttributes adds typed metry attributes at span start.
func WithAttributes(attrs ...metry.Attribute) Option {
	return func(c *config) {
		if len(attrs) == 0 {
			return
		}
		kv := make([]attribute.KeyValue, 0, len(attrs))
		for _, attr := range attrs {
			if otel := attrconv.ToOTel(attr); otel.Key != "" {
				kv = append(kv, otel)
			}
		}
		if len(kv) > 0 {
			c.otelOpts = append(c.otelOpts, trace.WithAttributes(kv...))
		}
	}
}

// OptionsToOTel converts typed options to OTel start options for internal span helpers.
func OptionsToOTel(opts ...Option) []trace.SpanStartOption {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg.otelOpts
}
