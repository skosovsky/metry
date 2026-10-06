package metry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/baggageutil"
	"github.com/skosovsky/metry/internal/enrichment"
	"github.com/skosovsky/metry/internal/traceutil"
)

// Enrich updates unified observability context: OTel baggage and active span attributes.
// Reassign ctx = metry.Enrich(...) on each call when accumulating attributes across multiple steps.
// Values are raw UTF-8 strings; wire percent encoding belongs to propagation.
// Invalid keys/values are skipped. Visible baggage retains a sorted fitting
// prefix within transport limits; private provenance and span/log values survive.
// ContextHandler reads the private provenance ledger for slog correlation.
func Enrich(ctx context.Context, attrs ...Attribute) context.Context {
	if len(attrs) == 0 {
		return ctx
	}

	valid := filterValidAttributes(attrs)
	if len(valid) == 0 {
		return ctx
	}

	values := make([]enrichment.Value, 0, len(valid))
	for _, attr := range valid {
		values = append(
			values,
			enrichment.Value{Key: attr.key, Text: attributeStringValue(attr), Type: attr.attrType(), Local: true},
		)
	}
	ctx = enrichment.Put(ctx, values...)
	ctx = enrichBaggage(ctx)
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
	// Preserve the existing W3C key contract without encoding the real value here.
	if _, err := baggage.NewMember(attr.key, "valid"); err != nil {
		return false
	}
	_, err := baggage.NewMemberRaw(attr.key, strVal)
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

func enrichBaggage(ctx context.Context) context.Context {
	members := baggage.FromContext(ctx).Members()
	// Rebuild from the full ledger, including values omitted by earlier overflow.
	// Foreign baggage stays transport-only and is never promoted to provenance.
	for _, value := range enrichment.Values(ctx) {
		typeProp, err := baggage.NewKeyValuePropertyRaw(baggageAttrTypeKey, value.Type)
		if err != nil {
			continue
		}
		member, err := baggage.NewMemberRaw(
			value.Key,
			value.Text,
			typeProp,
		)
		if err != nil {
			continue
		}
		members = append(members, member)
	}
	b, dropped := baggageutil.Build(members)
	baggageutil.Report(ctx, "local", dropped)
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
	out := make([]attributeFromBaggage, 0)
	for _, v := range enrichment.Values(ctx) {
		out = append(out, attributeFromBaggage{key: v.Key, value: v.Text, attrType: v.Type, local: v.Local})
	}
	return out
}

type attributeFromBaggage struct {
	key, value, attrType string
	local                bool
}
