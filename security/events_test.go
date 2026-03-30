package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/skosovsky/metry/testutil"
)

func TestRecordSecurityEvent_AddsEventToSpan(t *testing.T) {
	mem := testutil.SetupTestTracing(t)
	tracer := otel.Tracer("metry")
	ctx, span := tracer.Start(context.Background(), "test-op")
	RecordSecurityEvent(
		ctx,
		ActionBlock,
		"pii_masking",
		"PII detected in prompt",
		"POLICY_VIOLATION",
		"HIGH",
		false,
	)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "span should have one Security Intervention event")
	evt := spans[0].Events[0]
	assert.Equal(t, "Security Intervention", evt.Name)
	require.Len(t, evt.Attributes, 6)

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
	codeVal, ok := attrs.Value(CodeKey)
	require.True(t, ok)
	assert.Equal(t, "POLICY_VIOLATION", codeVal.AsString())
	severityVal, ok := attrs.Value(SeverityKey)
	require.True(t, ok)
	assert.Equal(t, "HIGH", severityVal.AsString())
}

func TestRecordSecurityEvent_EmptyCodeAndSeverity_AreNotAdded(t *testing.T) {
	mem := testutil.SetupTestTracing(t)
	tracer := otel.Tracer("metry")
	ctx, span := tracer.Start(context.Background(), "test-op")
	RecordSecurityEvent(
		ctx,
		ActionBlock,
		"pii_masking",
		"PII detected in prompt",
		"",
		"",
		false,
	)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "span should have one Security Intervention event")
	evt := spans[0].Events[0]
	require.Len(t, evt.Attributes, 4)

	attrs := attribute.NewSet(evt.Attributes...)
	_, ok := attrs.Value(CodeKey)
	assert.False(t, ok)
	_, ok = attrs.Value(SeverityKey)
	assert.False(t, ok)
}

func TestRecordSecurityEvent_OnlyCode_IsAdded(t *testing.T) {
	mem := testutil.SetupTestTracing(t)
	tracer := otel.Tracer("metry")
	ctx, span := tracer.Start(context.Background(), "test-op")
	RecordSecurityEvent(
		ctx,
		ActionBlock,
		"pii_masking",
		"PII detected in prompt",
		"POLICY_VIOLATION",
		"",
		false,
	)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "span should have one Security Intervention event")
	evt := spans[0].Events[0]
	require.Len(t, evt.Attributes, 5)

	attrs := attribute.NewSet(evt.Attributes...)
	codeVal, ok := attrs.Value(CodeKey)
	require.True(t, ok)
	assert.Equal(t, "POLICY_VIOLATION", codeVal.AsString())
	_, ok = attrs.Value(SeverityKey)
	assert.False(t, ok)
}

func TestRecordSecurityEvent_OnlySeverity_IsAdded(t *testing.T) {
	mem := testutil.SetupTestTracing(t)
	tracer := otel.Tracer("metry")
	ctx, span := tracer.Start(context.Background(), "test-op")
	RecordSecurityEvent(
		ctx,
		ActionBlock,
		"pii_masking",
		"PII detected in prompt",
		"",
		"HIGH",
		false,
	)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "span should have one Security Intervention event")
	evt := spans[0].Events[0]
	require.Len(t, evt.Attributes, 5)

	attrs := attribute.NewSet(evt.Attributes...)
	severityVal, ok := attrs.Value(SeverityKey)
	require.True(t, ok)
	assert.Equal(t, "HIGH", severityVal.AsString())
	_, ok = attrs.Value(CodeKey)
	assert.False(t, ok)
}

func TestRecordSecurityEvent_NoSpanInContext_DoesNotPanic(t *testing.T) {
	require.NotPanics(t, func() {
		RecordSecurityEvent(context.Background(), ActionPass, "test", "no span", "CODE", "HIGH", false)
	})
}
