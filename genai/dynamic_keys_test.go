package genai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/testutil"
)

func TestPayloadPoliciesProtectDynamicKeys(t *testing.T) {
	for _, policy := range []PayloadPolicy{RedactPayloadPolicy(), HashPayloadPolicy()} {
		// Arrange.
		secret := "fixture-secret-key"
		raw := `{"allowed":[{"fixture-secret-key":"fixture-secret-value"}]}`
		tracker, provider, exporter := newTestTracker(t, WithPayloadPolicy(policy))
		payload := Payload{
			InputMessages: []Message{
				{
					Role: "user",
					Parts: []ContentPart{
						{Type: "tool_call", ID: secret, Name: secret, Arguments: json.RawMessage(raw)},
					},
				},
			},
		}
		// Act.
		call := policy.SanitizeToolCall(ToolCall{Arguments: raw})
		result := policy.SanitizeToolResult(ToolResult{Status: OperationStatusOK, Result: raw})
		require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), payload, Usage{}))
		_, end, startErr := tracker.Runtime().StartTool(context.Background(), ToolCall{Name: "test", Arguments: raw})
		require.NoError(t, startErr)
		require.NoError(t, end.Finish(ToolResult{Status: OperationStatusOK, Result: raw}))
		flushTestProvider(t, provider)
		// Assert.
		assert.NotContains(t, call.Arguments, secret)
		assert.NotContains(t, result.Result, secret)
		for _, span := range exporter.GetSpans() {
			for _, attr := range span.Attributes {
				assert.NotContains(t, attr.Value.String(), secret)
				assert.NotContains(t, attr.Value.String(), "fixture-secret-value")
			}
		}
		assert.Contains(
			t,
			string(payload.InputMessages[0].Parts[0].Arguments),
			secret,
			"host input must remain unchanged",
		)
	}
}

func TestPayloadPolicyExplicitSchemaKeys(t *testing.T) {
	// Arrange.
	policy := RedactPayloadPolicy("allowed", "nested")
	raw := `{"allowed":{"nested":"secret"}}`
	// Act.
	result := policy.SanitizeToolCall(ToolCall{Arguments: raw})
	// Assert.
	assert.JSONEq(t, `{"allowed":{"nested":"[redacted]"}}`, result.Arguments)
	assert.Contains(t, RawPayloadPolicy().SanitizeToolCall(ToolCall{Arguments: raw}).Arguments, "secret")
	assert.JSONEq(
		t,
		`{"allowed":{"nested":"[redacted]"}}`,
		string(
			policy.SanitizePayload(Payload{SystemInstructions: []ContentPart{{Arguments: json.RawMessage(raw)}}}).SystemInstructions[0].Arguments,
		),
	)
}

func TestPayloadJSONInputIsBounded(t *testing.T) {
	// Arrange.
	policy := HashPayloadPolicy()
	inputs := []string{
		strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33),
		`{"key":"` + strings.Repeat("x", 1<<20) + `"}`,
	}
	// Act and Assert.
	for _, input := range inputs {
		result := policy.SanitizeToolCall(ToolCall{Arguments: input})
		assert.JSONEq(t, redactedPayloadJSON, result.Arguments)
	}
	// The regular recording path must use the same policy boundary.
	tracker, provider, exporter := newTestTracker(t, WithPayloadPolicy(policy))
	_, end, startErr := tracker.Runtime().StartTool(context.Background(), ToolCall{Name: "test", Arguments: inputs[0]})
	require.NoError(t, startErr)
	require.NoError(t, end.Finish(ToolResult{Status: OperationStatusOK, Result: inputs[1]}))
	flushTestProvider(t, provider)
	require.Len(t, exporter.GetSpans(), 1)
	assert.False(t, testutil.SpanStubHasAttr(exporter.GetSpans()[0], ToolCallArguments))
	assert.False(t, testutil.SpanStubHasAttr(exporter.GetSpans()[0], ToolCallResult))
	require.Len(t, exporter.GetSpans()[0].Events, 2)
}
