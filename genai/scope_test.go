package genai

import (
	"context"
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

func TestTrackerWithScope_DelegatesToPackageWithScope(t *testing.T) {
	tracker, _, _ := newTestTracker(t)
	ctx := tracker.WithScope(context.Background(), Scope{Provider: "openai", Model: "gpt-4o-mini"})
	got, ok := ScopeFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "openai", got.Provider)
	assert.Equal(t, "gpt-4o-mini", got.Model)
}
