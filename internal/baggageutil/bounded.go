// Package baggageutil builds deterministic bounded transport baggage.
package baggageutil

import (
	"context"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

// Limits match the pinned OTel v1.47.0 baggage implementation, including commas
// and percent-encoded properties. SDK upgrades must revalidate these bounds.
const (
	MaxMembers = 64
	MaxBytes   = 8192
)

// Build resolves duplicates last-wins, sorts keys and retains a fitting prefix.
// It never uses the SDK's unordered partial result on overflow.
func Build(members []baggage.Member) (baggage.Baggage, int) {
	unique := make(map[string]baggage.Member, len(members))
	for _, member := range members {
		unique[member.Key()] = member
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	retained := make([]baggage.Member, 0, min(len(keys), MaxMembers))
	bytes := 0
	for _, key := range keys {
		member := unique[key]
		encoded := member.String()
		if encoded == "" {
			continue
		}
		size := len(encoded)
		if len(retained) != 0 {
			size++
		}
		if len(retained) == MaxMembers || size > MaxBytes-bytes {
			break
		}
		retained = append(retained, member)
		bytes += size
	}
	bag, err := baggage.New(retained...)
	if err != nil {
		// A changed SDK bound must not reintroduce nondeterministic retention.
		return baggage.Baggage{}, len(keys)
	}
	return bag, len(keys) - len(retained)
}

// Report emits one fixed-size categorical event when a recording span exists.
// Keys, values and SDK error text are never included in diagnostics.
func Report(ctx context.Context, channel string, dropped int) {
	if dropped == 0 {
		return
	}
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.AddEvent("metry.baggage.dropped", trace.WithAttributes(
			attribute.String("metry.baggage.channel", channel),
			attribute.Int("metry.baggage.dropped_count", dropped),
		))
	}
}
