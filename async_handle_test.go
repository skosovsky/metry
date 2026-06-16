package metry

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/testutil"
)

func TestAsyncHandle_MarshalParse_RoundTrip(t *testing.T) {
	provider, _ := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "t", "root")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(ctx)
	require.NoError(t, err)
	end()

	token, err := handle.Marshal()
	require.NoError(t, err)

	parsed, err := ParseAsyncHandle(token)
	require.NoError(t, err)
	assert.True(t, parsed.IsValid())
	assert.True(t, handle.IsValid())

	token2, err := parsed.Marshal()
	require.NoError(t, err)
	assert.Equal(t, token, token2)
}

func TestNewAsyncHandle_NoSpan_ReturnsError(t *testing.T) {
	_, err := NewAsyncHandle(context.Background())
	require.ErrorIs(t, err, ErrNoSpanContext)
}

func TestRecordLinkedOutcomeWithProvider_RecordsSpan(t *testing.T) {
	ctx := context.Background()
	provider, mem := newTestProvider(t, WithServiceName("test"))

	spanCtx, end, err := provider.StartSpan(ctx, "t", "origin")
	require.NoError(t, err)
	originSC := trace.SpanFromContext(spanCtx).SpanContext()
	handle, err := NewAsyncHandle(spanCtx)
	require.NoError(t, err)
	end()

	err = handle.RecordLinkedOutcomeWithProvider(ctx, provider, "delivery.success", TenantID("t-1"))
	require.NoError(t, err)

	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	outcome := spans[1]
	assert.Equal(t, "delivery.success", outcome.Name)
	require.False(t, outcome.Parent.SpanID().IsValid(), "linked outcome must not set parent")
	require.NotEmpty(t, outcome.Links)
	assert.Equal(t, originSC.TraceID(), outcome.Links[0].SpanContext.TraceID())
	assert.Equal(t, originSC.SpanID(), outcome.Links[0].SpanContext.SpanID())
	assert.NotEqual(t, originSC.TraceID(), outcome.SpanContext.TraceID())

	assert.Equal(t, "t-1", testutil.SpanStubStringAttr(t, outcome, "tenant_id"))
	testutil.AssertSpanStubOkStatus(t, outcome)
}

func TestRecordLinkedOutcomeWithProvider_TypedAttributesOnSpan(t *testing.T) {
	ctx := context.Background()
	provider, mem := newTestProvider(t, WithServiceName("typed-outcome"))

	spanCtx, end, err := provider.StartSpan(ctx, "t", "origin")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(spanCtx)
	require.NoError(t, err)
	end()

	err = handle.RecordLinkedOutcomeWithProvider(
		ctx,
		provider,
		"eval.scored",
		FloatAttribute("score", 0.91),
		BoolAttribute("passed", true),
	)
	require.NoError(t, err)
	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	outcome := spans[1]
	assert.Equal(t, "eval.scored", outcome.Name)

	assert.InDelta(t, 0.91, testutil.SpanStubFloat64Attr(t, outcome, "score"), 1e-9)
	assert.True(t, testutil.SpanStubBoolAttr(t, outcome, "passed"))
	testutil.AssertSpanStubOkStatus(t, outcome)
}

func TestRecordLinkedOutcomeWithProvider_NilProvider_ReturnsErrNilProvider(t *testing.T) {
	provider, _ := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "t", "x")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(ctx)
	require.NoError(t, err)
	end()

	err = handle.RecordLinkedOutcomeWithProvider(context.Background(), nil, "x")
	require.ErrorIs(t, err, ErrNilProvider)
}

func TestRecordLinkedOutcomeWithProvider_NilTracerProvider_ReturnsErrNilTracerProvider(t *testing.T) {
	provider, _ := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "t", "x")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(ctx)
	require.NoError(t, err)
	end()

	nilTracerProvider := &Provider{otelTracer: nil}
	err = handle.RecordLinkedOutcomeWithProvider(context.Background(), nilTracerProvider, "x")
	require.ErrorIs(t, err, ErrNilTracerProvider)
}

func TestParseAsyncHandle_EmptyToken_ReturnsErrInvalidAsyncHandle(t *testing.T) {
	_, err := ParseAsyncHandle("")
	require.ErrorIs(t, err, ErrInvalidAsyncHandle)
}

func TestParseAsyncHandle_InvalidBase64_ReturnsErrInvalidAsyncHandle(t *testing.T) {
	_, err := ParseAsyncHandle("not-valid-base64!!!")
	require.ErrorIs(t, err, ErrInvalidAsyncHandle)
}

func TestParseAsyncHandle_TokenTooLarge_ReturnsErrHandleTokenTooLarge(t *testing.T) {
	token := strings.Repeat("A", 513)
	_, err := ParseAsyncHandle(token)
	require.ErrorIs(t, err, ErrHandleTokenTooLarge)
}

func TestRecordLinkedSpan_CallbackSetsAttributes(t *testing.T) {
	ctx := context.Background()
	provider, mem := newTestProvider(t, WithServiceName("test"))

	spanCtx, end, err := provider.StartSpan(ctx, "t", "origin")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(spanCtx)
	require.NoError(t, err)
	end()

	err = handle.RecordLinkedSpan(ctx, provider, "eval.result", func(w LinkedSpanWriter) error {
		w.SetAttributes(TenantID("t-1"), StringAttribute("metric", "faithfulness"))
		w.AddEvent("scored", FloatAttribute("score", 0.91))
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	outcome := spans[1]
	assert.Equal(t, "eval.result", outcome.Name)
	require.NotEmpty(t, outcome.Links)
	require.Len(t, outcome.Events, 1)
	event := outcome.Events[0]
	assert.Equal(t, "scored", event.Name)
	assert.InDelta(t, 0.91, testutil.SpanEventFloat64Attr(t, event.Attributes, "score"), 1e-9)

	assert.Equal(t, "t-1", testutil.SpanStubStringAttr(t, outcome, "tenant_id"))
	assert.Equal(t, "faithfulness", testutil.SpanStubStringAttr(t, outcome, "metric"))
	testutil.AssertSpanStubOkStatus(t, outcome)
}

func TestRecordLinkedSpan_Callback_NoActiveSpan_NoPanic(t *testing.T) {
	provider, _ := newTestProvider(t)
	spanCtx, end, err := provider.StartSpan(context.Background(), "t", "origin")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(spanCtx)
	require.NoError(t, err)
	end()

	require.NotPanics(t, func() {
		_ = handle.RecordLinkedSpan(context.Background(), provider, "eval.result", func(w LinkedSpanWriter) error {
			w.SetAttributes(TenantID("t-1"))
			w.AddEvent("scored", FloatAttribute("score", 0.5))
			return nil
		})
	})
}

func TestRecordLinkedSpan_CallbackError_SetsSpanStatus(t *testing.T) {
	ctx := context.Background()
	provider, mem := newTestProvider(t, WithServiceName("linked-err"))

	spanCtx, end, err := provider.StartSpan(ctx, "t", "origin")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(spanCtx)
	require.NoError(t, err)
	end()

	callbackErr := assert.AnError
	err = handle.RecordLinkedSpan(ctx, provider, "eval.result", func(LinkedSpanWriter) error {
		return callbackErr
	})
	require.ErrorIs(t, err, callbackErr)
	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	outcome := spans[1]
	assert.Equal(t, "eval.result", outcome.Name)
	testutil.AssertSpanStubErrorStatus(t, outcome)
}

func TestRecordLinkedSpan_InvalidHandle_ReturnsErrInvalidAsyncHandle(t *testing.T) {
	provider, _ := newTestProvider(t)
	err := AsyncHandle{}.RecordLinkedSpan(context.Background(), provider, "x", func(LinkedSpanWriter) error {
		return nil
	})
	require.ErrorIs(t, err, ErrInvalidAsyncHandle)
}

func TestRecordLinkedOutcomeWithProvider_InvalidHandle_ReturnsErrInvalidAsyncHandle(t *testing.T) {
	provider, _ := newTestProvider(t)
	err := AsyncHandle{}.RecordLinkedOutcomeWithProvider(context.Background(), provider, "x")
	require.ErrorIs(t, err, ErrInvalidAsyncHandle)
}

func TestRecordLinkedSpan_NilProvider_ReturnsErrNilProvider(t *testing.T) {
	provider, _ := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "t", "x")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(ctx)
	require.NoError(t, err)
	end()

	err = handle.RecordLinkedSpan(context.Background(), nil, "x", func(LinkedSpanWriter) error { return nil })
	require.ErrorIs(t, err, ErrNilProvider)
}

func TestRecordLinkedSpan_NilTracerProvider_ReturnsErrNilTracerProvider(t *testing.T) {
	provider, _ := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "t", "x")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(ctx)
	require.NoError(t, err)
	end()

	nilTracerProvider := &Provider{otelTracer: nil}
	err = handle.RecordLinkedSpan(
		context.Background(),
		nilTracerProvider,
		"x",
		func(LinkedSpanWriter) error { return nil },
	)
	require.ErrorIs(t, err, ErrNilTracerProvider)
}

func TestProvider_InjectToMap_NilProvider_NoOp(t *testing.T) {
	var provider *Provider
	carrier := map[string]any{"biz": "data"}
	provider.InjectToMap(context.Background(), carrier)
	assert.Equal(t, "data", carrier["biz"])
}

func TestProvider_ExtractFromMap_NilProvider_ReturnsOriginalContext(t *testing.T) {
	var provider *Provider
	ctx := context.Background()
	got := provider.ExtractFromMap(ctx, map[string]any{"k": "v"})
	assert.Equal(t, ctx, got)
}

func TestRecordLinkedOutcomeWithProvider_SkipsInvalidAttribute(t *testing.T) {
	ctx := context.Background()
	provider, mem := newTestProvider(t, WithServiceName("invalid-attr-outcome"))

	spanCtx, end, err := provider.StartSpan(ctx, "t", "origin")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(spanCtx)
	require.NoError(t, err)
	end()

	err = handle.RecordLinkedOutcomeWithProvider(
		ctx,
		provider,
		"delivery.success",
		Attribute{},
		TenantID("t-1"),
	)
	require.NoError(t, err)
	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	outcome := spans[1]
	assert.Equal(t, "t-1", testutil.SpanStubStringAttr(t, outcome, "tenant_id"))
	assert.False(t, testutil.SpanStubHasAttr(outcome, ""))
	testutil.AssertSpanStubOkStatus(t, outcome)
}

func TestProviderStartSpan_SkipsInvalidWithSpanAttributes(t *testing.T) {
	provider, mem := newTestProvider(t, WithServiceName("invalid-start-attrs"))

	ctx, end, err := provider.StartSpan(
		context.Background(),
		"t",
		"request",
		WithSpanAttributes(Attribute{}, TenantID("t-1")),
	)
	require.NoError(t, err)
	end()
	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "t-1", testutil.SpanStubStringAttr(t, spans[0], "tenant_id"))
}
