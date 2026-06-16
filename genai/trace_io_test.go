package genai

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/testutil"
)

func TestRecordTraceIO_WritesInputAndOutputAttributes(t *testing.T) {
	tracker, provider, mem := newTestTracker(t, WithRecordPayloads(true))

	input := Payload{
		InputMessages: []Message{{Role: "user", Parts: []ContentPart{{Type: "text", Content: "question"}}}},
	}
	output := Payload{
		OutputMessages: []Message{{Role: "assistant", Parts: []ContentPart{{Type: "text", Content: "answer"}}}},
	}

	ctx, end, err := provider.StartSpan(context.Background(), "trace-io", "mirror")
	require.NoError(t, err)
	tracker.RecordTraceIO(ctx, input, output)
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Contains(t, testutil.SpanStubStringAttr(t, spans[0], InputMessages), "question")
	assert.Contains(t, testutil.SpanStubStringAttr(t, spans[0], OutputMessages), "answer")
}

func TestRecordTraceIO_PayloadRecordingDisabled_SkipsAttributes(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	ctx, end, err := provider.StartSpan(context.Background(), "trace-io", "mirror")
	require.NoError(t, err)
	tracker.RecordTraceIO(ctx, testPayload(), testPayload())
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.False(t, testutil.SpanStubHasAttr(spans[0], InputMessages))
}

func TestRecordTraceIO_TruncatesLongPayload(t *testing.T) {
	const limit = 128
	tracker, provider, mem := newTestTracker(t,
		WithRecordPayloads(true),
		WithMaxContextLength(limit),
	)

	longText := strings.Repeat("x", limit+50)
	input := Payload{
		InputMessages: []Message{{Role: "user", Parts: []ContentPart{{Type: "text", Content: longText}}}},
	}

	ctx, end, err := provider.StartSpan(context.Background(), "trace-io", "mirror")
	require.NoError(t, err)
	tracker.RecordTraceIO(ctx, input, Payload{})
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	out := testutil.SpanStubStringAttr(t, spans[0], InputMessages)
	assert.LessOrEqual(t, len(out), limit)
	assert.True(t, strings.HasSuffix(out, truncationSuffix))
	assert.True(t, utf8.ValidString(out))
}
