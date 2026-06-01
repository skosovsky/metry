package genai

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRecordTraceIO_WritesInputAndOutputAttributes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(
		mp.Meter("trace-io"),
		tp.Tracer("trace-io"),
		WithRecordPayloads(true),
	)
	require.NoError(t, err)

	input := Payload{
		InputMessages: []Message{{Role: "user", Parts: []ContentPart{{Type: "text", Content: "question"}}}},
	}
	output := Payload{
		OutputMessages: []Message{{Role: "assistant", Parts: []ContentPart{{Type: "text", Content: "answer"}}}},
	}

	_, span := tp.Tracer("trace-io").Start(context.Background(), "mirror")
	tracker.RecordTraceIO(context.Background(), span, input, output)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	assert.Contains(t, mustStringAttr(t, attrs, InputMessagesKey), "question")
	assert.Contains(t, mustStringAttr(t, attrs, OutputMessagesKey), "answer")
}

func TestRecordTraceIO_PayloadRecordingDisabled_SkipsAttributes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracker, err := NewTracker(mp.Meter("trace-io"), tp.Tracer("trace-io"))
	require.NoError(t, err)

	_, span := tp.Tracer("trace-io").Start(context.Background(), "mirror")
	tracker.RecordTraceIO(context.Background(), span, testPayload(), testPayload())
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	_, ok := attrs.Value(InputMessagesKey)
	assert.False(t, ok)
}

func TestRecordTraceIO_TruncatesLongPayload(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	const limit = 128
	tracker, err := NewTracker(
		mp.Meter("trace-io"),
		tp.Tracer("trace-io"),
		WithRecordPayloads(true),
		WithMaxContextLength(limit),
	)
	require.NoError(t, err)

	longText := strings.Repeat("x", limit+50)
	input := Payload{
		InputMessages: []Message{{Role: "user", Parts: []ContentPart{{Type: "text", Content: longText}}}},
	}

	_, span := tp.Tracer("trace-io").Start(context.Background(), "mirror")
	tracker.RecordTraceIO(context.Background(), span, input, Payload{})
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := attribute.NewSet(spans[0].Attributes...)
	out := mustStringAttr(t, attrs, InputMessagesKey)
	assert.LessOrEqual(t, len(out), limit)
	assert.True(t, strings.HasSuffix(out, truncationSuffix))
	assert.True(t, utf8.ValidString(out))
}
