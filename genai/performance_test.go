package genai

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"go.opentelemetry.io/otel/attribute"

	"github.com/skosovsky/metry/internal/enrichment"
)

//nolint:gochecknoglobals // Benchmark sink keeps the result observable.
var performanceScopeSink Scope

//nolint:gochecknoglobals // Benchmark sink keeps the result observable.
var performanceStringSink string

func performanceScopeContext(n int, complete bool) context.Context {
	values := make([]enrichment.Value, 0, n+4)
	for i := range n {
		values = append(
			values,
			enrichment.Value{Key: fmt.Sprintf("unrelated%d", i), Text: "value", Type: "string", Local: true},
		)
	}
	if !complete {
		values = append(values,
			enrichment.Value{Key: scopeBaggageProvider, Text: "fallback-provider", Type: "string", Local: true},
			enrichment.Value{Key: scopeBaggageModel, Text: "fallback-model", Type: "string", Local: true},
			enrichment.Value{Key: scopeBaggageOperation, Text: "fallback-operation", Type: "string"},
			enrichment.Value{Key: scopeBaggagePurpose, Text: "fallback-purpose", Type: "string"})
	}
	ctx := enrichment.Put(context.Background(), values...)
	scope := Scope{Provider: "private-provider"}
	if complete {
		scope = Scope{
			Provider:  "private-provider",
			Model:     "private-model",
			Operation: "private-operation",
			Purpose:   "private-purpose",
		}
	}
	return WithScope(ctx, scope)
}

func TestScopeLookupAllocationBudget(t *testing.T) {
	for _, n := range []int{0, 8, 64, 256} {
		for _, complete := range []bool{true, false} {
			t.Run(fmt.Sprintf("%d/complete=%t", n, complete), func(t *testing.T) {
				// Arrange.
				ctx := performanceScopeContext(n, complete)
				want := Scope{
					Provider:  "private-provider",
					Model:     "fallback-model",
					Operation: "fallback-operation",
					Purpose:   "fallback-purpose",
				}
				if complete {
					want = Scope{
						Provider:  "private-provider",
						Model:     "private-model",
						Operation: "private-operation",
						Purpose:   "private-purpose",
					}
				}
				// Act.
				var got Scope
				var ok bool
				allocations := testing.AllocsPerRun(100, func() { got, ok = ScopeFromContext(ctx) })
				// Assert.
				if !ok || got != want || allocations != 0 {
					t.Fatalf("got=%+v ok=%t allocs=%g want=%+v", got, ok, allocations, want)
				}
			})
		}
	}
}

// A context spy proves populated fields cause no fallback access, independently
// of an allocation check (a copying-free but unnecessary lookup would pass it).
type performanceLookupContext struct {
	context.Context

	ledgerReads int
}

func (c *performanceLookupContext) Value(key any) any {
	if _, ok := key.(scopeContextKey); !ok {
		c.ledgerReads++
	}
	return c.Context.Value(key)
}
func TestScopeOnlyLooksUpMissingFields(t *testing.T) {
	for _, complete := range []bool{true, false} {
		// Arrange.
		ctx := &performanceLookupContext{Context: performanceScopeContext(8, complete)}
		// Act.
		got, ok := ScopeFromContext(ctx)
		// Assert.
		wantReads := 3
		if complete {
			wantReads = 0
		}
		if !ok || got.Provider != "private-provider" || ctx.ledgerReads != wantReads {
			t.Fatalf("complete=%t reads=%d want=%d scope=%+v", complete, ctx.ledgerReads, wantReads, got)
		}
	}
}

func TestDimensionCachePerformanceBound(t *testing.T) {
	// Arrange.
	profile := testInstrumentationProfile()
	profile.SeriesBudget = 32
	d := newMetricDimensions(profile)
	d.allowed["performance"] = make(map[string]struct{})
	for i := range 10000 {
		d.allowed["performance"][strconv.Itoa(i)] = struct{}{}
	}
	// Act.
	for i := range 10000 {
		d.normalize(OperationDurationMetricName, []attribute.KeyValue{attribute.String("performance", strconv.Itoa(i))})
	}
	// Assert.
	if got := len(d.combinations[OperationDurationMetricName]); got != 29 {
		t.Fatalf("retained=%d want=29", got)
	}
}

func BenchmarkPerformanceScope(b *testing.B) {
	for _, n := range []int{0, 8, 64, 256} {
		for _, mode := range []string{"complete", "partial", "string"} {
			b.Run(fmt.Sprintf("%s/%d", mode, n), func(b *testing.B) {
				ctx := performanceScopeContext(n, mode == "complete")
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if mode == "string" {
						performanceStringSink = enrichment.String(ctx, scopeBaggagePurpose)
					} else {
						performanceScopeSink, _ = ScopeFromContext(ctx)
					}
				}
			})
		}
	}
}

func TestScopeRejectsTypedNonStrings(t *testing.T) {
	// Arrange: trusted provenance alone does not make a scalar a scope string.
	ctx := enrichment.Put(context.Background(),
		enrichment.Value{Key: scopeBaggageProvider, Text: "7", Type: "int", Local: true},
		enrichment.Value{Key: scopeBaggageModel, Text: "true", Type: "bool", Local: true},
		enrichment.Value{Key: scopeBaggageOperation, Text: "1.5", Type: "float"},
		enrichment.Value{Key: scopeBaggagePurpose, Text: "text", Type: "invalid"})
	// Act.
	got, ok := ScopeFromContext(ctx)
	// Assert.
	if ok || got != (Scope{}) {
		t.Fatalf("typed scalar became scope: %+v ok=%t", got, ok)
	}
}
