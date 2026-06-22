package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/testutil"
)

func TestRedactPayloadPolicy_RedactsMessagePayload(t *testing.T) {
	tracker, provider, mem := newTestTracker(t, WithPayloadPolicy(RedactPayloadPolicy()))
	payload := Payload{
		InputMessages: []Message{{
			Role:  "user",
			Parts: []ContentPart{{Type: "text", Content: "secret prompt"}},
		}},
	}

	err := tracker.RecordInteraction(context.Background(), testMeta(), payload, Usage{})

	require.NoError(t, err)
	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	value := testutil.SpanStubStringAttr(t, spans[0], InputMessages)
	assert.Contains(t, value, redactedPayloadText)
	assert.NotContains(t, value, "secret prompt")
}

func TestHashPayloadPolicy_FingerprintsMessagePayload(t *testing.T) {
	tracker, provider, mem := newTestTracker(t, WithPayloadPolicy(HashPayloadPolicy()))
	payload := Payload{
		InputMessages: []Message{{
			Role:  "user",
			Parts: []ContentPart{{Type: "text", Content: "secret prompt"}},
		}},
	}

	err := tracker.RecordInteraction(context.Background(), testMeta(), payload, Usage{})

	require.NoError(t, err)
	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	value := testutil.SpanStubStringAttr(t, spans[0], InputMessages)
	assert.Contains(t, value, "sha256")
	assert.NotContains(t, value, "secret prompt")
}

func TestToolPayloadPolicy_RedactsArgumentsAndResult(t *testing.T) {
	tracker, provider, mem := newTestTracker(t, WithPayloadPolicy(RedactPayloadPolicy()))
	recorder := tracker.Runtime()

	ctx, end := recorder.StartTool(context.Background(), ToolCall{
		Name:      "search",
		CallID:    "call-1",
		Arguments: `{"secret":"arg","nested":{"id":123}}`,
	})
	recorder.RecordToolResult(ctx, ToolResult{Result: `{"secret":"result","items":[1,"two"]}`})
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	args := testutil.SpanStubStringAttr(t, spans[0], ToolCallArguments)
	result := testutil.SpanStubStringAttr(t, spans[0], ToolCallResult)
	assert.Contains(t, args, "redacted")
	assert.Contains(t, result, "redacted")
	assert.Contains(t, args, "nested")
	assert.Contains(t, result, "items")
	assert.NotContains(t, args, "arg")
	assert.NotContains(t, result, "result")
}

func TestWithRawPayloads_ExportsToolPayloadsExplicitly(t *testing.T) {
	tracker, provider, mem := newTestTracker(t, WithRawPayloads())
	recorder := tracker.Runtime()

	ctx, end := recorder.StartTool(context.Background(), ToolCall{
		Name:      "search",
		CallID:    "call-1",
		Arguments: `{"secret":"arg"}`,
	})
	recorder.RecordToolResult(ctx, ToolResult{Result: `{"secret":"result"}`})
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Contains(t, testutil.SpanStubStringAttr(t, spans[0], ToolCallArguments), "arg")
	assert.Contains(t, testutil.SpanStubStringAttr(t, spans[0], ToolCallResult), "result")
}
