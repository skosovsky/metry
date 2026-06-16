package genai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/testutil"
)

func TestRecordInteraction_WithPayloadAndUsage_SetsAttributes(t *testing.T) {
	tracker, provider, mem := newTestTracker(t, WithRecordPayloads(true))

	ctx := context.Background()
	require.NoError(t, tracker.RecordInteraction(ctx, testMeta(), testPayload(), Usage{
		InputTokens:           10,
		OutputTokens:          20,
		ReasoningOutputTokens: 4,
		Cost:                  0.25,
		Currency:              "CREDITS",
	}))

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "openai", testutil.SpanStubStringAttr(t, spans[0], ProviderName))
	assert.Equal(t, "chat", testutil.SpanStubStringAttr(t, spans[0], OperationName))
	assert.Equal(t, int64(10), testutil.SpanStubInt64Attr(t, spans[0], InputTokens))
	assert.Equal(t, int64(20), testutil.SpanStubInt64Attr(t, spans[0], OutputTokens))
	assert.Equal(t, int64(4), testutil.SpanStubInt64Attr(t, spans[0], UsageReasoningOutputTokens))
	assert.InDelta(t, 0.25, testutil.SpanStubFloat64Attr(t, spans[0], UsageCost), 1e-9)
	assert.Equal(t, "CREDITS", testutil.SpanStubStringAttr(t, spans[0], CostCurrency))
}

func TestRecordInteraction_TruncatesPayloadString(t *testing.T) {
	tracker, provider, mem := newTestTracker(t,
		WithRecordPayloads(true),
		WithMaxContextLength(96),
	)

	payload := Payload{
		InputMessages: []Message{{
			Role: "user",
			Parts: []ContentPart{{
				Type:    "text",
				Content: strings.Repeat("a", 2048),
			}},
		}},
	}

	require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), payload, Usage{}))

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	value := testutil.SpanStubStringAttr(t, spans[0], InputMessages)
	assert.LessOrEqual(t, len(value), 96)
	assert.True(t, utf8.ValidString(value))
}

func TestStartToolSpan_AndRecordToolResult_SetToolAttributes(t *testing.T) {
	tracker, provider, mem := newTestTracker(t, WithMaxContextLength(64))

	ctx, end := tracker.StartToolSpan(context.Background(), "search", "call-1", `{"q":`)
	tracker.RecordToolResult(ctx, `{"result":"`+strings.Repeat("b", 256)+`"}`, errors.New("tool execution failed"))
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "execute_tool", testutil.SpanStubStringAttr(t, spans[0], OperationName))
	assert.Equal(t, "search", testutil.SpanStubStringAttr(t, spans[0], ToolName))
	assert.Equal(t, "call-1", testutil.SpanStubStringAttr(t, spans[0], ToolCallID))
	arguments := testutil.SpanStubStringAttr(t, spans[0], ToolCallArguments)
	assert.NotEmpty(t, arguments)
	assert.Contains(t, arguments, `{"q":`)
	assert.False(t, json.Valid([]byte(arguments)))
	assert.NotEmpty(t, testutil.SpanStubStringAttr(t, spans[0], ToolCallResult))
	assert.True(t, testutil.SpanStubBoolAttr(t, spans[0], ToolError))
	testutil.AssertSpanStubErrorStatus(t, spans[0])
}

func TestRecordToolResult_Success_SetsOkStatus(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	ctx, end := tracker.StartToolSpan(context.Background(), "search", "call-ok", `{"q":"x"}`)
	tracker.RecordToolResult(ctx, `{"result":"ok"}`, nil)
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.False(t, testutil.SpanStubBoolAttr(t, spans[0], ToolError))
	testutil.AssertSpanStubOkStatus(t, spans[0])
}

func TestStartToolSpan_EndOnly_SetsOkStatus(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	_, end := tracker.StartToolSpan(context.Background(), "search", "call-end-only", `{"q":"x"}`)
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	testutil.AssertSpanStubOkStatus(t, spans[0])
}

func TestRecordInteraction_ErrorType_SetsSpanErrorStatus(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	require.NoError(t, tracker.RecordInteraction(context.Background(), Meta{
		Provider:  "openai",
		Operation: "chat",
		ErrorType: "timeout",
	}, Payload{}, Usage{InputTokens: 1}))

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "timeout", testutil.SpanStubStringAttr(t, spans[0], ErrorType))
	testutil.AssertSpanStubErrorStatus(t, spans[0])
}

func TestRecordInteraction_Success_SetsOkStatus(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), Payload{}, Usage{InputTokens: 1}))

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	testutil.AssertSpanStubOkStatus(t, spans[0])
}

func TestRecordCacheHit_NoSpan_NoPanic(t *testing.T) {
	tracker, _, _ := newTestTracker(t)
	require.NotPanics(t, func() {
		tracker.RecordCacheHit(context.Background(), true, "cache")
	})
}

func TestStartToolSpan_WithKeepHint_ExportsSpanWhenBaseSamplerDrops(t *testing.T) {
	tracker, provider, mem := newTestTrackerWithSampler(t, NewHintSampler(metry.NeverSample()))

	_, end := tracker.StartToolSpan(
		context.Background(),
		"search",
		"call-keep",
		`{"q":"hint"}`,
		WithSpanSamplingKeep(),
	)
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "search", testutil.SpanStubStringAttr(t, spans[0], ToolName))
	assert.True(t, spans[0].SpanContext.IsSampled())
}

func TestStartToolSpan_WithCallerAttributes_PreservesBuiltInAttributes(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	_, end := tracker.StartToolSpan(
		context.Background(),
		"search",
		"call-attrs",
		`{"q":"hello"}`,
		WithSpanAttributes(metry.StringAttribute("test.caller.attr", "present")),
	)
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "search", testutil.SpanStubStringAttr(t, spans[0], ToolName))
	assert.Equal(t, "execute_tool", testutil.SpanStubStringAttr(t, spans[0], OperationName))
	assert.Equal(t, "present", testutil.SpanStubStringAttr(t, spans[0], "test.caller.attr"))
}

func TestStartToolSpan_WithDuplicateCallerKeys_BuiltInWins(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	_, end := tracker.StartToolSpan(
		context.Background(),
		"search",
		"call-dup",
		`{"q":"hello"}`,
		WithSpanAttributes(
			metry.StringAttribute(OperationName, "override"),
			metry.StringAttribute(ToolName, "override"),
			metry.StringAttribute(ToolCallID, "override"),
		),
	)
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "execute_tool", testutil.SpanStubStringAttr(t, spans[0], OperationName))
	assert.Equal(t, "search", testutil.SpanStubStringAttr(t, spans[0], ToolName))
	assert.Equal(t, "call-dup", testutil.SpanStubStringAttr(t, spans[0], ToolCallID))
}

func TestRecordInteraction_TruncatedPayload_MayBeInvalidJSON_ButUTF8Safe(t *testing.T) {
	tracker, provider, mem := newTestTracker(t,
		WithRecordPayloads(true),
		WithMaxContextLength(80),
	)

	payload := Payload{
		InputMessages: []Message{{
			Role: "user",
			Parts: []ContentPart{{
				Type:    "text",
				Content: strings.Repeat("你", 512),
			}},
		}},
	}

	require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), payload, Usage{}))

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	value := testutil.SpanStubStringAttr(t, spans[0], InputMessages)
	assert.LessOrEqual(t, len(value), 80)
	assert.True(t, utf8.ValidString(value))
	assert.True(t, strings.HasSuffix(value, truncationSuffix))
	assert.False(t, json.Valid([]byte(value)))
}

func TestRecordCacheHit_AndRecordAgentStep(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)

	ctx, end, err := provider.StartSpan(context.Background(), "genai-test", "span")
	require.NoError(t, err)
	tracker.RecordCacheHit(ctx, true, "pgvector_cache")
	tracker.RecordAgentStep(ctx, "cardiologist", "specialist", "step-2")
	end()

	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.True(t, testutil.SpanStubBoolAttr(t, spans[0], CacheHit))
	assert.Equal(t, "pgvector_cache", testutil.SpanStubStringAttr(t, spans[0], RetrievalSource))
	require.Len(t, spans[0].Events, 1)
	assert.Equal(t, AgentStepEvent, spans[0].Events[0].Name)
}

func testMeta() Meta {
	return Meta{
		Provider:      "openai",
		Operation:     "chat",
		RequestModel:  "gpt-4o-mini",
		ResponseModel: "gpt-4o-mini",
		Duration:      2 * time.Second,
	}
}

func testPayload() Payload {
	return Payload{
		SystemInstructions: []ContentPart{{
			Type:    "text",
			Content: "You are concise.",
		}},
		InputMessages: []Message{{
			Role: "user",
			Parts: []ContentPart{{
				Type:    "text",
				Content: "What is 2+2?",
			}},
		}},
		OutputMessages: []Message{{
			Role: "assistant",
			Parts: []ContentPart{{
				Type:    "text",
				Content: "4",
			}},
			FinishReason: "stop",
		}},
	}
}
