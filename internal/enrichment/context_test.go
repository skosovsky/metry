package enrichment

import (
	"context"
	"sync"
	"testing"
)

func TestImmutableLedgerOwnership(t *testing.T) {
	// Arrange.
	incoming := []Value{{Key: "k", Text: "parent", Type: "string", Local: true}}
	parent := Put(context.Background(), incoming...)
	// Act.
	incoming[0].Text = "caller"
	snapshot := Values(parent)
	snapshot[0].Text = "snapshot"
	child := Put(parent, Value{Key: "k", Text: "remote", Type: "string"}, Value{Key: "new", Text: "child", Type: "string", Local: true})
	replaced := Put(parent, Value{Key: "k", Text: "replacement", Type: "string", Local: true})
	// Assert.
	if String(parent, "k") != "parent" || String(child, "k") != "parent" || String(replaced, "k") != "replacement" || String(parent, "new") != "" {
		t.Fatal("ledger ownership or local precedence changed")
	}
}

func TestStringTypesAndAllocations(t *testing.T) {
	// Arrange.
	ctx := Put(context.Background(), Value{Key: "s", Text: "text", Type: "string"}, Value{Key: "i", Text: "42", Type: "int", Local: true})
	// Act.
	var got string
	allocations := testing.AllocsPerRun(100, func() { got = String(ctx, "s") })
	// Assert.
	if got != "text" || allocations != 0 || String(ctx, "i") != "" || String(ctx, "absent") != "" {
		t.Fatalf("got=%q allocs=%g", got, allocations)
	}
}

func TestConcurrentLedgerReadsAndCopies(t *testing.T) {
	// Arrange.
	parent := Put(context.Background(), Value{Key: "k", Text: "parent", Type: "string", Local: true})
	var wg sync.WaitGroup
	// Act.
	for range 8 {
		wg.Go(func() {
			for range 100 {
				snapshot := Values(parent)
				snapshot[0].Text = "changed"
				child := Put(parent, Value{Key: "k", Text: "child", Type: "string", Local: true})
				// Assert inside each concurrent operation.
				if String(parent, "k") != "parent" || String(child, "k") != "child" {
					t.Error("shared ledger mutated")
				}
			}
		})
	}
	wg.Wait()
}
