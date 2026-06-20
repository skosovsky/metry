package genai

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

type fakeProviderAdapter struct {
	reqPayload  Payload
	reqMeta     Meta
	reqErr      error
	respPayload Payload
	respUsage   Usage
	respErr     error
}

func (a fakeProviderAdapter) ParseRequest(_ any) (Payload, Meta, error) {
	if a.reqErr != nil {
		return Payload{}, Meta{}, a.reqErr
	}
	return a.reqPayload, a.reqMeta, nil
}

func (a fakeProviderAdapter) ParseResponse(_ any) (Payload, Usage, error) {
	if a.respErr != nil {
		return Payload{}, Usage{}, a.respErr
	}
	return a.respPayload, a.respUsage, nil
}

func TestRecordModelInteraction_HappyPath_MatchesDirectRecordInteraction(t *testing.T) {
	tracker, provider, memMetric, memTrace := newTestTrackerWithMetrics(t, WithRawPayloads())

	adapter := fakeProviderAdapter{
		reqPayload: Payload{
			InputMessages: []Message{{Role: "user", Parts: []ContentPart{{Type: "text", Content: "hi"}}}},
		},
		reqMeta: testMeta(),
		respPayload: Payload{
			OutputMessages: []Message{{Role: "assistant", Parts: []ContentPart{{Type: "text", Content: "hello"}}}},
		},
		respUsage: Usage{InputTokens: 10, OutputTokens: 20, Cost: 0.001},
	}

	err := tracker.RecordModelInteraction(context.Background(), adapter, "raw-req", "raw-resp")
	require.NoError(t, err)

	flushTestProvider(t, provider)
	spans := memTrace.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "openai", testutil.SpanStubStringAttr(t, spans[0], ProviderName))
	assert.Equal(t, int64(10), testutil.SpanStubInt64Attr(t, spans[0], InputTokens))
	assert.Equal(t, int64(20), testutil.SpanStubInt64Attr(t, spans[0], OutputTokens))
	assert.Contains(t, testutil.SpanStubStringAttr(t, spans[0], InputMessages), "hi")
	assert.Contains(t, testutil.SpanStubStringAttr(t, spans[0], OutputMessages), "hello")

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	assert.InDelta(
		t,
		10,
		metrytest.Int64HistogramSumByAttr(t, rm, TokenUsageMetricName, TokenType, TokenTypeInput),
		1e-9,
	)
	assert.InDelta(
		t,
		20,
		metrytest.Int64HistogramSumByAttr(t, rm, TokenUsageMetricName, TokenType, TokenTypeOutput),
		1e-9,
	)
}

func TestRecordModelInteraction_WithScope_UsesScopeDefaults(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	adapter := fakeProviderAdapter{
		reqPayload:  testPayload(),
		reqMeta:     Meta{},
		respPayload: Payload{},
		respUsage:   Usage{InputTokens: 1},
	}

	ctx := WithScope(context.Background(), Scope{
		Provider:  "scope-openai",
		Operation: "scope-chat",
		Model:     "scope-model",
	})
	err := tracker.RecordModelInteraction(ctx, adapter, "raw-req", "raw-resp")
	require.NoError(t, err)

	flushTestProvider(t, provider)
	span := mem.GetSpans()[0]
	assert.Equal(t, "scope-openai", testutil.SpanStubStringAttr(t, span, ProviderName))
	assert.Equal(t, "scope-chat", testutil.SpanStubStringAttr(t, span, OperationName))
}

func TestRecordModelInteraction_ParseRequestError_ReturnsError(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	parseErr := errors.New("bad request shape")
	adapter := fakeProviderAdapter{reqErr: parseErr}

	err := tracker.RecordModelInteraction(context.Background(), adapter, "raw", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, parseErr)

	flushTestProvider(t, provider)
	assert.Empty(t, mem.GetSpans())
}

func TestRecordModelInteraction_ParseResponseError_ReturnsError(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	parseErr := errors.New("bad response shape")
	adapter := fakeProviderAdapter{
		reqMeta: testMeta(),
		respErr: parseErr,
	}

	err := tracker.RecordModelInteraction(context.Background(), adapter, "raw", "raw")
	require.Error(t, err)
	require.ErrorIs(t, err, parseErr)

	flushTestProvider(t, provider)
	assert.Empty(t, mem.GetSpans())
}

func TestRecordModelInteraction_NilAdapter_ReturnsError(t *testing.T) {
	tracker, _, _ := newTestTracker(t)

	err := tracker.RecordModelInteraction(context.Background(), nil, "raw", "raw")
	require.ErrorIs(t, err, ErrAdapterRequired)
}
