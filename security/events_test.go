package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestRecordSecurityEventWithProvider_ActiveSpan_AddsEvent(t *testing.T) {
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("security-test"))
	ctx, end, err := provider.StartSpan(context.Background(), "metry", "test-op")
	require.NoError(t, err)
	err = RecordSecurityEventWithProvider(
		ctx,
		provider,
		ActionBlock,
		"pii_masking",
		"PII detected in prompt",
		"POLICY_VIOLATION",
		"HIGH",
		false,
	)
	require.NoError(t, err)
	end()
	flushSpans(t, provider)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "span should have one Security Intervention event")
	evt := spans[0].Events[0]
	assert.Equal(t, "Security Intervention", evt.Name)
	require.Len(t, evt.Attributes, 6)

	assert.Equal(t, ActionBlock, testutil.SpanEventStringAttr(t, evt.Attributes, Action))
	assert.Equal(t, "pii_masking", testutil.SpanEventStringAttr(t, evt.Attributes, Validator))
	assert.Equal(t, "PII detected in prompt", testutil.SpanEventStringAttr(t, evt.Attributes, Reason))
	assert.False(t, testutil.SpanStubBoolAttr(t, testutil.AttrsStub(evt.Attributes), ShadowMode))
	assert.Equal(t, "POLICY_VIOLATION", testutil.SpanEventStringAttr(t, evt.Attributes, Code))
	assert.Equal(t, "HIGH", testutil.SpanEventStringAttr(t, evt.Attributes, Severity))
	testutil.AssertSpanStubOkStatus(t, spans[0])
}

func TestRecordSecurityEventWithProvider_EmptyCodeAndSeverity_AreNotAdded(t *testing.T) {
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("security-test"))
	ctx, end, err := provider.StartSpan(context.Background(), "metry", "test-op")
	require.NoError(t, err)
	err = RecordSecurityEventWithProvider(
		ctx,
		provider,
		ActionBlock,
		"pii_masking",
		"PII detected in prompt",
		"",
		"",
		false,
	)
	require.NoError(t, err)
	end()
	flushSpans(t, provider)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "span should have one Security Intervention event")
	evt := spans[0].Events[0]
	require.Len(t, evt.Attributes, 4)

	eventStub := testutil.AttrsStub(evt.Attributes)
	assert.False(t, testutil.SpanStubHasAttr(eventStub, Code))
	assert.False(t, testutil.SpanStubHasAttr(eventStub, Severity))
}

func TestRecordSecurityEventWithProvider_OnlyCode_IsAdded(t *testing.T) {
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("security-test"))
	ctx, end, err := provider.StartSpan(context.Background(), "metry", "test-op")
	require.NoError(t, err)
	err = RecordSecurityEventWithProvider(
		ctx,
		provider,
		ActionBlock,
		"pii_masking",
		"PII detected in prompt",
		"POLICY_VIOLATION",
		"",
		false,
	)
	require.NoError(t, err)
	end()
	flushSpans(t, provider)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "span should have one Security Intervention event")
	evt := spans[0].Events[0]
	require.Len(t, evt.Attributes, 5)

	assert.Equal(t, "POLICY_VIOLATION", testutil.SpanEventStringAttr(t, evt.Attributes, Code))
	assert.False(t, testutil.SpanStubHasAttr(testutil.AttrsStub(evt.Attributes), Severity))
}

func TestRecordSecurityEventWithProvider_OnlySeverity_IsAdded(t *testing.T) {
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("security-test"))
	ctx, end, err := provider.StartSpan(context.Background(), "metry", "test-op")
	require.NoError(t, err)
	err = RecordSecurityEventWithProvider(
		ctx,
		provider,
		ActionBlock,
		"pii_masking",
		"PII detected in prompt",
		"",
		"HIGH",
		false,
	)
	require.NoError(t, err)
	end()
	flushSpans(t, provider)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1, "span should have one Security Intervention event")
	evt := spans[0].Events[0]
	require.Len(t, evt.Attributes, 5)

	assert.Equal(t, "HIGH", testutil.SpanEventStringAttr(t, evt.Attributes, Severity))
	assert.False(t, testutil.SpanStubHasAttr(testutil.AttrsStub(evt.Attributes), Code))
}

func flushSpans(t *testing.T, provider *metry.Provider) {
	t.Helper()
	require.NoError(t, provider.ForceFlush(context.Background()))
}

func TestRecordSecurityEventWithProvider_NilProvider_ReturnsErrNilProvider(t *testing.T) {
	err := RecordSecurityEventWithProvider(
		context.Background(),
		nil,
		ActionPass,
		"test",
		"no provider",
		"",
		"",
		false,
	)
	require.ErrorIs(t, err, metry.ErrNilProvider)
}

func TestRecordSecurityEventWithProvider_NoSpan_CreatesSpan(t *testing.T) {
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("security-withprovider"))
	err := RecordSecurityEventWithProvider(
		context.Background(),
		provider,
		ActionBlock,
		"pii_masking",
		"PII detected",
		"POLICY_VIOLATION",
		"HIGH",
		false,
	)
	require.NoError(t, err)
	flushSpans(t, provider)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "security.intervention", spans[0].Name)
	require.Len(t, spans[0].Events, 1)
	assert.Equal(t, "Security Intervention", spans[0].Events[0].Name)
	testutil.AssertSpanStubOkStatus(t, spans[0])
}

func TestRecordSecurityEventWithProvider_NilTracer_ReturnsErrNilTracerProvider(t *testing.T) {
	provider := metrytest.NewProviderWithDeps(nil, nil)
	err := RecordSecurityEventWithProvider(
		context.Background(),
		provider,
		ActionPass,
		"test",
		"no tracer",
		"",
		"",
		false,
	)
	require.ErrorIs(t, err, metry.ErrNilTracerProvider)
}
