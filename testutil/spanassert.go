package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// SpanStringAttr returns a string attribute from a span attribute set.
func SpanStringAttr(t *testing.T, attrs attribute.Set, key string) string {
	t.Helper()
	v, ok := attrs.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("missing string attr %q", key)
	}
	return v.AsString()
}

// SpanInt64Attr returns an int64 attribute from a span attribute set.
func SpanInt64Attr(t *testing.T, attrs attribute.Set, key string) int64 {
	t.Helper()
	v, ok := attrs.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("missing int attr %q", key)
	}
	return v.AsInt64()
}

// SpanFloat64Attr returns a float64 attribute from a span attribute set.
func SpanFloat64Attr(t *testing.T, attrs attribute.Set, key string) float64 {
	t.Helper()
	v, ok := attrs.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("missing float attr %q", key)
	}
	return v.AsFloat64()
}

// SpanBoolAttr returns a bool attribute from a span attribute set.
func SpanBoolAttr(t *testing.T, attrs attribute.Set, key string) bool {
	t.Helper()
	v, ok := attrs.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("missing bool attr %q", key)
	}
	return v.AsBool()
}

// SpanHasAttr reports whether the attribute key exists on the set.
func SpanHasAttr(attrs attribute.Set, key string) bool {
	_, ok := attrs.Value(attribute.Key(key))
	return ok
}

// SpanEventStringAttr returns a string attribute from a span event attribute list.
func SpanEventStringAttr(t *testing.T, attrs []attribute.KeyValue, key string) string {
	t.Helper()
	return SpanStringAttr(t, attribute.NewSet(attrs...), key)
}

// SpanEventFloat64Attr returns a float64 attribute from a span event attribute list.
func SpanEventFloat64Attr(t *testing.T, attrs []attribute.KeyValue, key string) float64 {
	t.Helper()
	return SpanFloat64Attr(t, attribute.NewSet(attrs...), key)
}

// AssertSpanStubOkStatus fails unless the span stub has OTel Ok status.
func AssertSpanStubOkStatus(t *testing.T, span tracetest.SpanStub) {
	t.Helper()
	require.Equal(t, codes.Ok, span.Status.Code, "expected span ok status")
}

// SpanStubStringAttr returns a string attribute from a span stub.
func SpanStubStringAttr(t *testing.T, span tracetest.SpanStub, key string) string {
	t.Helper()
	return SpanStringAttr(t, attribute.NewSet(span.Attributes...), key)
}

// SpanStubInt64Attr returns an int64 attribute from a span stub.
func SpanStubInt64Attr(t *testing.T, span tracetest.SpanStub, key string) int64 {
	t.Helper()
	return SpanInt64Attr(t, attribute.NewSet(span.Attributes...), key)
}

// SpanStubFloat64Attr returns a float64 attribute from a span stub.
func SpanStubFloat64Attr(t *testing.T, span tracetest.SpanStub, key string) float64 {
	t.Helper()
	return SpanFloat64Attr(t, attribute.NewSet(span.Attributes...), key)
}

// SpanStubBoolAttr returns a bool attribute from a span stub.
func SpanStubBoolAttr(t *testing.T, span tracetest.SpanStub, key string) bool {
	t.Helper()
	return SpanBoolAttr(t, attribute.NewSet(span.Attributes...), key)
}

// SpanStubFloat64SliceAttr returns a float64 slice attribute from a span stub.
func SpanStubFloat64SliceAttr(t *testing.T, span tracetest.SpanStub, key string) []float64 {
	t.Helper()
	attrs := attribute.NewSet(span.Attributes...)
	v, ok := attrs.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("missing float slice attr %q", key)
	}
	return v.AsFloat64Slice()
}

// SpanStubHasAttr reports whether the span stub has the attribute key.
func SpanStubHasAttr(span tracetest.SpanStub, key string) bool {
	return SpanHasAttr(attribute.NewSet(span.Attributes...), key)
}

// SpanByName returns the first span stub with the given name.
func SpanByName(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	for _, span := range spans {
		if span.Name == name {
			return span
		}
	}
	t.Fatalf("span %q not found", name)
	return tracetest.SpanStub{}
}

// AssertSpanLinksTo verifies span has a link to the given span context.
func AssertSpanLinksTo(t *testing.T, span tracetest.SpanStub, linked trace.SpanContext) {
	t.Helper()
	for _, link := range span.Links {
		sc := link.SpanContext
		if sc.TraceID() == linked.TraceID() && sc.SpanID() == linked.SpanID() {
			return
		}
	}
	t.Fatalf("expected link to trace_id=%v span_id=%v, got links=%v", linked.TraceID(), linked.SpanID(), span.Links)
}

// SpansContainParentChildRelation reports whether spans include a parent-child relation.
func SpansContainParentChildRelation(spans tracetest.SpanStubs) bool {
	for i := range spans {
		if !spans[i].Parent.SpanID().IsValid() {
			continue
		}
		for j := range spans {
			if i == j {
				continue
			}
			if spans[i].SpanContext.TraceID() != spans[j].SpanContext.TraceID() {
				continue
			}
			if spans[i].Parent.SpanID() == spans[j].SpanContext.SpanID() {
				return true
			}
		}
	}
	return false
}

// AssertSpanStubErrorStatus fails unless the span stub has OTel Error status.
func AssertSpanStubErrorStatus(t *testing.T, span tracetest.SpanStub) {
	t.Helper()
	require.Equal(t, codes.Error, span.Status.Code, "expected span error status")
}
