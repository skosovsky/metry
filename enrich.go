package metry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"
)

// Enrich updates unified observability context: OTel baggage and active span attributes.
// Reassign ctx = metry.Enrich(...) on each call when accumulating attributes across multiple steps.
// Invalid baggage members for individual attributes are skipped without failing the whole call.
// ContextHandler reads enrich attributes from baggage for slog correlation.
func Enrich(ctx context.Context, attrs ...Attribute) context.Context {
	if len(attrs) == 0 {
		return ctx
	}

	valid := filterValidAttributes(attrs)
	if len(valid) == 0 {
		return ctx
	}

	ctx = enrichBaggage(ctx, valid)
	enrichSpan(ctx, valid)
	return ctx
}

func validAttribute(attr Attribute) bool {
	if attr.key == "" {
		return false
	}
	strVal := attributeStringValue(attr)
	if attr.f64 == nil && attr.b == nil && attr.i64 == nil && attr.value == "" {
		return false
	}
	_, err := baggage.NewMember(attr.key, strVal)
	return err == nil
}

func filterValidAttributes(attrs []Attribute) []Attribute {
	out := make([]Attribute, 0, len(attrs))
	for _, attr := range attrs {
		if validAttribute(attr) {
			out = append(out, attr)
		}
	}
	return out
}

func enrichBaggage(ctx context.Context, attrs []Attribute) context.Context {
	b := baggage.FromContext(ctx)
	for _, attr := range attrs {
		strVal := attributeStringValue(attr)
		typeProp, err := baggage.NewKeyValuePropertyRaw(baggageAttrTypeKey, attr.attrType())
		if err != nil {
			continue
		}
		member, err := baggage.NewMemberRaw(
			attr.key,
			strVal,
			typeProp,
		)
		if err != nil {
			continue
		}
		next, err := b.SetMember(member)
		if err != nil {
			continue
		}
		b = next
	}
	return baggage.ContextWithBaggage(ctx, b)
}

func enrichSpan(ctx context.Context, attrs []Attribute) {
	traceutil.MutateRecordingSpan(trace.SpanFromContext(ctx), func(span trace.Span) {
		kv := make([]attribute.KeyValue, 0, len(attrs))
		for _, attr := range attrs {
			if attr.key == "" {
				continue
			}
			otelKV := attr.toOTel()
			if otelKV.Key == "" {
				continue
			}
			kv = append(kv, otelKV)
		}
		if len(kv) > 0 {
			span.SetAttributes(kv...)
		}
	})
}

func slogAttrsFromBaggage(ctx context.Context) []attributeFromBaggage {
	bag := baggage.FromContext(ctx)
	members := bag.Members()
	if len(members) == 0 {
		return nil
	}
	out := make([]attributeFromBaggage, 0, len(members))
	for _, m := range members {
		if m.Key() == "" {
			continue
		}
		attrType := ""
		for _, prop := range m.Properties() {
			if prop.Key() == baggageAttrTypeKey {
				if v, ok := prop.Value(); ok {
					attrType = v
				}
				break
			}
		}
		if attrType == "" {
			continue
		}
		out = append(out, attributeFromBaggage{key: m.Key(), value: m.Value(), attrType: attrType})
	}
	return out
}

type attributeFromBaggage struct {
	key, value, attrType string
}
