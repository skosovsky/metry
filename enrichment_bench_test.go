package metry

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkEnrichLocal(b *testing.B) {
	for _, count := range []int{8, 70} {
		b.Run(fmt.Sprintf("attributes%d", count), func(b *testing.B) {
			attrs := make([]Attribute, 0, count)
			for index := range count {
				attrs = append(attrs, StringAttribute(fmt.Sprintf("key%03d", index), "value"))
			}
			ctx := context.Background()
			b.ReportAllocs()
			for b.Loop() {
				_ = Enrich(ctx, attrs...)
			}
		})
	}
}
