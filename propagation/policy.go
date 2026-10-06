package propagation

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"go.opentelemetry.io/otel/baggage"
	otelprop "go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/enrichment"
)

// PolicyConfig separately controls remote trace acceptance and business values.
type PolicyConfig struct {
	RejectRemoteTrace                                   bool
	IngressBaggageKeys, EgressBaggageKeys, ReservedKeys []string
}

// Policy is immutable. Its zero value accepts trace context only.
type Policy struct {
	reject                    bool
	ingress, egress, reserved map[string]bool
}

func NewPolicy(cfg PolicyConfig) (Policy, error) {
	p := Policy{
		reject:   cfg.RejectRemoteTrace,
		ingress:  map[string]bool{},
		egress:   map[string]bool{},
		reserved: map[string]bool{},
	}
	for _, pair := range []struct {
		keys []string
		dst  map[string]bool
	}{{cfg.IngressBaggageKeys, p.ingress}, {cfg.EgressBaggageKeys, p.egress}, {cfg.ReservedKeys, p.reserved}} {
		for _, key := range pair.keys {
			if strings.TrimSpace(key) != key || key == "" {
				return Policy{}, errors.New("invalid propagation policy key")
			}
			if _, err := baggage.NewMember(key, "value"); err != nil {
				return Policy{}, errors.New("invalid propagation policy key")
			}
			pair.dst[key] = true
		}
	}
	return p, nil
}

// AllowsRemoteTrace reports whether remote trace continuation may be accepted.
// It applies to durable continuation as well as transport extraction.
func (p Policy) AllowsRemoteTrace() bool { return !p.reject }

func reserved(key string) bool {
	switch key {
	case "msg", "time", "level", "source", "trace_id", "span_id":
		return true
	default:
		return false
	}
}

type policyPropagator struct {
	base   otelprop.TextMapPropagator
	policy Policy
}

// Wrap controls only the declared transport callbacks; direct host SDK writes are host-owned.
func Wrap(base otelprop.TextMapPropagator, policy Policy) otelprop.TextMapPropagator {
	if nilInterface(base) {
		base = otelprop.NewCompositeTextMapPropagator()
	}
	return policyPropagator{base: base, policy: policy}
}

func (p policyPropagator) Fields() []string { return append([]string(nil), p.base.Fields()...) }

func (p policyPropagator) Inject(ctx context.Context, carrier otelprop.TextMapCarrier) {
	if nilInterface(carrier) {
		return
	}
	members := make([]baggage.Member, 0)
	for _, v := range enrichment.Values(ctx) {
		if !p.policy.egress[v.Key] {
			continue
		}
		prop, err := baggage.NewKeyValuePropertyRaw(enrichment.TypeKey, v.Type)
		if err != nil {
			continue
		}
		member, err := baggage.NewMemberRaw(v.Key, v.Text, prop)
		if err == nil {
			members = append(members, member)
		}
	}
	bag, _ := baggage.New(members...)
	clean := baggage.ContextWithBaggage(ctx, bag)
	for _, field := range p.Fields() {
		carrier.Set(field, "")
	}
	staged := otelprop.MapCarrier{}
	p.base.Inject(clean, staged)
	for _, field := range p.Fields() {
		if value := staged.Get(field); value != "" {
			carrier.Set(field, value)
		}
	}
}

func (p policyPropagator) Extract(ctx context.Context, carrier otelprop.TextMapCarrier) context.Context {
	if nilInterface(carrier) {
		return ctx
	}
	originalTrace := trace.SpanFromContext(ctx)
	clean := baggage.ContextWithBaggage(ctx, baggage.Baggage{})
	staged := otelprop.MapCarrier{}
	for _, field := range p.Fields() {
		if value := carrier.Get(field); value != "" {
			staged.Set(field, value)
		}
	}
	extracted := p.base.Extract(clean, staged)
	if p.policy.reject {
		extracted = trace.ContextWithSpan(extracted, originalTrace)
	}
	// Custom callbacks cannot promote their baggage to trusted values without this allowlist.
	imported := make([]enrichment.Value, 0)
	for _, m := range baggage.FromContext(extracted).Members() {
		if !p.policy.ingress[m.Key()] || reserved(m.Key()) || p.policy.reserved[m.Key()] {
			continue
		}
		kind := ""
		for _, prop := range m.Properties() {
			if prop.Key() == enrichment.TypeKey {
				kind, _ = prop.Value()
				break
			}
		}
		v := enrichment.Value{Key: m.Key(), Text: m.Value(), Type: kind, Local: false}
		if enrichment.ValidScalar(v) {
			imported = append(imported, v)
		}
	}
	extracted = enrichment.Put(extracted, imported...)
	// Expose only trusted values as baggage; manually supplied baggage is not promoted.
	members := make([]baggage.Member, 0)
	for _, v := range enrichment.Values(extracted) {
		prop, _ := baggage.NewKeyValuePropertyRaw(enrichment.TypeKey, v.Type)
		m, err := baggage.NewMemberRaw(v.Key, v.Text, prop)
		if err == nil {
			members = append(members, m)
		}
	}
	bag, _ := baggage.New(members...)
	return baggage.ContextWithBaggage(extracted, bag)
}

// nilInterface also recognizes typed nil interface values at public boundaries.
func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
