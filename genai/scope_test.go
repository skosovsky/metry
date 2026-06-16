package genai

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/testutil"
)

func TestWithScope_EmptyScope_NoOp(t *testing.T) {
	ctx := WithScope(context.Background(), Scope{})
	_, ok := ScopeFromContext(ctx)
	assert.False(t, ok)
}

func TestWithScope_PurposeOnly_RecordInteraction_SetsPurposeOnSpan(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	ctx := WithScope(context.Background(), Scope{Purpose: PurposeQualityEvaluation})
	require.NoError(t, tracker.RecordInteraction(ctx, Meta{}, Payload{}, Usage{InputTokens: 1}))

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, PurposeQualityEvaluation, testutil.SpanStubStringAttr(t, spans[0], OperationPurpose))
}

func TestWithScope_StoresGenAIBaggageKeys(t *testing.T) {
	ctx := WithScope(context.Background(), Scope{Provider: "openai", Model: "gpt-4o-mini"})
	got, ok := ScopeFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "openai", got.Provider)
	assert.Equal(t, "gpt-4o-mini", got.Model)
}

func TestWithScope_RecordInteraction_UsesScopeDefaults(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	ctx := WithScope(context.Background(), Scope{
		Provider:  "openai",
		Model:     "gpt-4o-mini",
		Operation: "chat",
		Purpose:   PurposeGeneration,
	})
	require.NoError(t, tracker.RecordInteraction(ctx, Meta{}, testPayload(), Usage{InputTokens: 1}))

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "openai", testutil.SpanStubStringAttr(t, spans[0], ProviderName))
	assert.Equal(t, "chat", testutil.SpanStubStringAttr(t, spans[0], OperationName))
	assert.Equal(t, PurposeGeneration, testutil.SpanStubStringAttr(t, spans[0], OperationPurpose))
}

func TestWithScope_ExplicitMetaOverridesScope(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	ctx := WithScope(context.Background(), Scope{Provider: "scope-provider"})
	require.NoError(t, tracker.RecordInteraction(ctx, Meta{Provider: "explicit"}, Payload{}, Usage{}))

	flushTestProvider(t, provider)
	assert.Equal(t, "explicit", testutil.SpanStubStringAttr(t, mem.GetSpans()[0], ProviderName))
}

func TestRecordOperation_PropagatesScope(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	scope := Scope{Provider: "anthropic", Operation: "chat"}
	err := tracker.RecordOperation(context.Background(), scope, func(ctx context.Context) error {
		got, ok := ScopeFromContext(ctx)
		require.True(t, ok)
		assert.Equal(t, "anthropic", got.Provider)
		return nil
	})
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "genai.operation", spans[0].Name)
	assert.Equal(t, "anthropic", testutil.SpanStubStringAttr(t, spans[0], ProviderName))
	assert.Equal(t, "chat", testutil.SpanStubStringAttr(t, spans[0], OperationName))
}

func TestRecordOperation_SetsScopeAttributesOnSpan(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	scope := Scope{
		Provider:  "openai",
		Model:     "gpt-4o-mini",
		Operation: "chat",
		Purpose:   PurposeGeneration,
	}
	require.NoError(t, tracker.RecordOperation(context.Background(), scope, func(context.Context) error {
		return nil
	}))

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "openai", testutil.SpanStubStringAttr(t, spans[0], ProviderName))
	assert.Equal(t, "gpt-4o-mini", testutil.SpanStubStringAttr(t, spans[0], ResponseModel))
	assert.Equal(t, PurposeGeneration, testutil.SpanStubStringAttr(t, spans[0], OperationPurpose))
}

func TestTrackerWithScope_DelegatesToPackageWithScope(t *testing.T) {
	tracker, _, _ := newTestTracker(t)
	ctx := tracker.WithScope(context.Background(), Scope{Provider: "openai", Model: "gpt-4o-mini"})
	got, ok := ScopeFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "openai", got.Provider)
	assert.Equal(t, "gpt-4o-mini", got.Model)
}

func TestRecordOperation_ReturnsFnError(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	fnErr := errors.New("boom")
	err := tracker.RecordOperation(context.Background(), Scope{}, func(context.Context) error {
		return fnErr
	})
	require.ErrorIs(t, err, fnErr)

	flushTestProvider(t, provider)
	require.Len(t, mem.GetSpans(), 1)
	testutil.AssertSpanStubErrorStatus(t, mem.GetSpans()[0])
}

func TestRecordOperation_Success_SetsOkStatus(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	require.NoError(t, tracker.RecordOperation(context.Background(), Scope{
		Provider:  "openai",
		Operation: "chat",
	}, func(context.Context) error {
		return nil
	}))

	flushTestProvider(t, provider)
	require.Len(t, mem.GetSpans(), 1)
	testutil.AssertSpanStubOkStatus(t, mem.GetSpans()[0])
}
