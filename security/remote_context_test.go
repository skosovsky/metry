package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
)

func TestSecurityEventRemoteContextCreatesLocalChild(t *testing.T) {
	// Arrange: a valid sampled remote parent has no local recording span.
	provider, exporter := metrytest.NewTestProvider(t)
	ctx := provider.ExtractFromMap(context.Background(), map[string]any{
		"traceparent": "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
	})
	parent := trace.SpanContextFromContext(ctx)
	require.True(t, parent.IsRemote())
	require.False(t, trace.SpanFromContext(ctx).IsRecording())

	// Act.
	err := Record(ctx, provider, Observation{Action: ActionBlock, Validator: "validator", Reason: "reason"})
	flushSpans(t, provider)

	// Assert: context validity must not silently swallow the event.
	require.NoError(t, err)
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, parent.TraceID(), spans[0].SpanContext.TraceID())
	assert.Equal(t, parent.SpanID(), spans[0].Parent.SpanID())
	require.Len(t, spans[0].Events, 1)
	assert.Equal(t, "Security Intervention", spans[0].Events[0].Name)
}

func TestSecurityEventEndedSpanCreatesChild(t *testing.T) {
	// Arrange.
	provider, exporter := metrytest.NewTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "test", "finished")
	require.NoError(t, err)
	parent := trace.SpanContextFromContext(ctx)
	end()

	// Act.
	err = Record(ctx, provider, Observation{Action: ActionBlock, Validator: "validator", Reason: "reason"})
	flushSpans(t, provider)

	// Assert.
	require.NoError(t, err)
	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	assert.Empty(t, spans[0].Events)
	assert.Equal(t, parent.SpanID(), spans[1].Parent.SpanID())
	require.Len(t, spans[1].Events, 1)
}

func TestSecurityEventRemoteContextRespectsSampler(t *testing.T) {
	// Arrange.
	provider, exporter := metrytest.NewTestProvider(t, metry.WithSampler(metry.NeverSample()))
	ctx := provider.ExtractFromMap(context.Background(), map[string]any{
		"traceparent": "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
	})

	// Act.
	err := Record(ctx, provider, Observation{Action: ActionBlock, Validator: "validator", Reason: "reason"})
	flushSpans(t, provider)

	// Assert.
	require.NoError(t, err)
	assert.Empty(t, exporter.GetSpans())
}
