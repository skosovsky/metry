package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// ProducerSpanStub wraps a span context as a span stub for link assertions.
func ProducerSpanStub(sc trace.SpanContext) tracetest.SpanStub {
	return tracetest.SpanStub{SpanContext: sc} //nolint:exhaustruct // partial stub for link assertions
}

func AttrsStub(attrs []attribute.KeyValue) tracetest.SpanStub {
	return tracetest.SpanStub{Attributes: attrs} //nolint:exhaustruct // partial stub for event attrs
}

// AssertSpanStubLinksTo verifies span links to a producer span stub.
func AssertSpanStubLinksTo(t *testing.T, span, producer tracetest.SpanStub) {
	t.Helper()
	AssertSpanLinksTo(t, span, producer.SpanContext)
}

// AssertLinkBasedAsyncSpan verifies link-based async span semantics.
func AssertLinkBasedAsyncSpan(t *testing.T, span, producer tracetest.SpanStub) {
	t.Helper()
	AssertSpanStubLinksTo(t, span, producer)
	require.False(t, span.Parent.SpanID().IsValid(), "link-based async span must not set parent")
	require.NotEqual(
		t,
		producer.SpanContext.TraceID(),
		span.SpanContext.TraceID(),
		"async span should start a new trace",
	)
}

// NewTestParentSpanContext returns a synthetic sampled span context for async link tests.
func NewTestParentSpanContext(remote bool) trace.SpanContext {
	traceID := trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	spanID := trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2}
	return trace.NewSpanContext(trace.SpanContextConfig{ //nolint:exhaustruct // synthetic test span context
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     remote,
	})
}

// NewUnsampledRemoteParentSpanContext returns an unsampled remote span context for async tests.
func NewUnsampledRemoteParentSpanContext() trace.SpanContext {
	traceID := trace.TraceID{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5}
	spanID := trace.SpanID{6, 6, 6, 6, 6, 6, 6, 6}
	return trace.NewSpanContext(trace.SpanContextConfig{ //nolint:exhaustruct // synthetic test span context
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: 0,
		Remote:     true,
	})
}
