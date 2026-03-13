package security

import (
	"context"
	"testing"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestRecordSecurityEvent_AddsEventToSpan(t *testing.T) {
	mem := testutil.SetupTestTracing(t)
	tracer := metry.GlobalTracer()
	ctx, span := tracer.Start(context.Background(), "test-op")
	RecordSecurityEvent(ctx, ActionBlock, "pii_masking", "PII detected in prompt", false)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "span should have one Security Intervention event")
	evt := spans[0].Events[0]
	assert.Equal(t, "Security Intervention", evt.Name)

	attrs := attribute.NewSet(evt.Attributes...)
	actionVal, ok := attrs.Value(ActionKey)
	require.True(t, ok)
	assert.Equal(t, ActionBlock, actionVal.AsString())
	validatorVal, ok := attrs.Value(ValidatorKey)
	require.True(t, ok)
	assert.Equal(t, "pii_masking", validatorVal.AsString())
	reasonVal, ok := attrs.Value(ReasonKey)
	require.True(t, ok)
	assert.Equal(t, "PII detected in prompt", reasonVal.AsString())
	shadowVal, ok := attrs.Value(ShadowModeKey)
	require.True(t, ok)
	assert.False(t, shadowVal.AsBool())
}

func TestRecordSecurityEvent_NoSpanInContext_DoesNotPanic(t *testing.T) {
	require.NotPanics(t, func() {
		RecordSecurityEvent(context.Background(), ActionPass, "test", "no span", false)
	})
}
