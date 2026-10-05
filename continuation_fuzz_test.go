package metry_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
)

func FuzzParseAsyncHandle(f *testing.F) {
	valid, err := metry.NoopAsyncHandle().Marshal()
	if err != nil {
		f.Fatal(err)
	}
	for _, token := range []string{"", "%%%", "e30", valid, strings.Repeat("a", 2049)} {
		f.Add(token)
	}
	f.Fuzz(func(t *testing.T, token string) {
		handle, err := metry.ParseAsyncHandle(token)
		if len(token) > 2048 && !errors.Is(err, metry.ErrHandleTokenTooLarge) {
			t.Fatal("encoded size bound not enforced")
		}
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if !handle.IsValid() {
			t.Fatal("parser accepted invalid handle")
		}
		encoded, err := handle.Marshal()
		if err != nil || len(encoded) > 2048 {
			t.Fatal("accepted handle cannot round-trip within bounds")
		}
		again, err := metry.ParseAsyncHandle(encoded)
		if err != nil || !again.IsValid() {
			t.Fatal("invalid canonical round-trip")
		}
	})
}

func FuzzParseTraceSnapshot(f *testing.F) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
	snapshot, err := metry.TraceSnapshotFromContext(trace.ContextWithSpanContext(context.Background(), sc))
	if err != nil {
		f.Fatal(err)
	}
	valid, err := snapshot.Marshal()
	if err != nil {
		f.Fatal(err)
	}
	for _, token := range []string{"", "%%%", "e30", valid, strings.Repeat("a", 2049)} {
		f.Add(token)
	}
	f.Fuzz(func(t *testing.T, token string) {
		snapshot, err := metry.ParseTraceSnapshot(token)
		if len(token) > 2048 && !errors.Is(err, metry.ErrTraceSnapshotTokenTooLarge) {
			t.Fatal("encoded size bound not enforced")
		}
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if !snapshot.IsValid() {
			t.Fatal("parser accepted invalid snapshot")
		}
		encoded, err := snapshot.Marshal()
		if err != nil || len(encoded) > 2048 {
			t.Fatal("accepted snapshot cannot round-trip within bounds")
		}
		again, err := metry.ParseTraceSnapshot(encoded)
		if err != nil || !again.IsValid() {
			t.Fatal("invalid canonical round-trip")
		}
	})
}
