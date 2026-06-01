package genai

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("adapter"),
		tp.Tracer("adapter"),
		WithRecordPayloads(true),
	)
	require.NoError(t, err)

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

	ctx := context.Background()
	_, span := tp.Tracer("adapter").Start(ctx, "chat")
	err = tracker.RecordModelInteraction(ctx, span, adapter, "raw-req", "raw-resp")
	require.NoError(t, err)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Equal(t, "openai", mustStringAttr(t, attrs, ProviderNameKey))
	assert.Equal(t, int64(10), mustIntAttr(t, attrs, InputTokensKey))
	assert.Equal(t, int64(20), mustIntAttr(t, attrs, OutputTokensKey))
	assert.Contains(t, mustStringAttr(t, attrs, InputMessagesKey), "hi")
	assert.Contains(t, mustStringAttr(t, attrs, OutputMessagesKey), "hello")

	rm := collectMetrics(t, reader)
	assert.InDelta(t, 10, int64HistogramSumByTokenType(t, rm, TokenUsageMetricName, TokenTypeInput), 1e-9)
	assert.InDelta(t, 20, int64HistogramSumByTokenType(t, rm, TokenUsageMetricName, TokenTypeOutput), 1e-9)
}

func TestRecordModelInteraction_ParseRequestError_SetsSpanError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("adapter"), tp.Tracer("adapter"))
	require.NoError(t, err)

	parseErr := errors.New("bad request shape")
	adapter := fakeProviderAdapter{reqErr: parseErr}

	_, span := tp.Tracer("adapter").Start(context.Background(), "chat")
	err = tracker.RecordModelInteraction(context.Background(), span, adapter, "raw", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, parseErr)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status.Code)
}

func TestRecordModelInteraction_ParseResponseError_SetsSpanError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("adapter"), tp.Tracer("adapter"))
	require.NoError(t, err)

	parseErr := errors.New("bad response shape")
	adapter := fakeProviderAdapter{
		reqMeta: testMeta(),
		respErr: parseErr,
	}

	_, span := tp.Tracer("adapter").Start(context.Background(), "chat")
	err = tracker.RecordModelInteraction(context.Background(), span, adapter, "raw", "raw")
	require.Error(t, err)
	require.ErrorIs(t, err, parseErr)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status.Code)
}

func TestRecordModelInteraction_NilAdapter_ReturnsError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("adapter"), tp.Tracer("adapter"))
	require.NoError(t, err)

	_, span := tp.Tracer("adapter").Start(context.Background(), "chat")
	defer span.End()
	err = tracker.RecordModelInteraction(context.Background(), span, nil, "raw", "raw")
	require.Error(t, err)
}
