package genai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPayloadJSONNumericLexemes(t *testing.T) {
	for _, number := range []string{"9007199254740992", "9007199254740993", "9223372036854775807", "-9223372036854775808", strings.Repeat("9", 400), "0.12345678901234567890123456789", "1.234567890123456789e+1000", "1", "1.0", "1e0"} {
		t.Run(number, func(t *testing.T) {
			// Arrange: raw JSON avoids any host float conversion.
			raw := `[` + number + `,"` + strings.Repeat("x", 300) + `"]`
			// Act: overflow retains the first numeric element.
			encoded, err := boundedJSON(json.RawMessage(raw), max(128, len(number)+40))
			// Assert: retained numeric spelling is exact, including very large values.
			require.NoError(t, err)
			assert.Contains(t, encoded, `"data":[`+number+`]`)
			value, err := decodePayloadJSON(number)
			require.NoError(t, err)
			require.IsType(t, json.Number(""), value)
			assert.Equal(t, number, value.(json.Number).String())
			assert.Equal(t, `"`+fingerprintPayloadLabel(number)+`"`, sanitizeJSONString(number, hashJSONLeaf))
			assert.Equal(t, `"[redacted]"`, sanitizeJSONString(number, redactJSONLeaf))
		})
	}
}

func TestPayloadJSONFingerprintsPreserveLexicalNumbers(t *testing.T) {
	// Arrange: the previous float64 intermediate collapsed adjacent integers.
	first := `{"n":9007199254740992}`
	second := `{"n":9007199254740993}`
	// Act.
	firstHash := HashPayloadPolicy().SanitizeToolCall(ToolCall{Arguments: first}).Arguments
	secondHash := HashPayloadPolicy().SanitizeToolCall(ToolCall{Arguments: second}).Arguments
	// Assert: structure canonicalization retains numeric spelling.
	assert.NotEqual(t, firstHash, secondHash)
	assert.Equal(
		t,
		sanitizeJSONString(`{"b":2,"a":1}`, hashJSONLeaf),
		sanitizeJSONString(`{"a":1,"b":2}`, hashJSONLeaf),
	)
	assert.NotEqual(t, sanitizeJSONString("1", hashJSONLeaf), sanitizeJSONString("1.0", hashJSONLeaf))
	allowed := payloadSchemaKeys([]string{"n"})
	assert.Contains(
		t,
		sanitizeJSONStringWithKeys(first, hashJSONLeaf, allowed),
		fingerprintPayloadLabel("9007199254740992"),
	)
	assert.JSONEq(t, `{"n":"[redacted]"}`, sanitizeJSONStringWithKeys(first, redactJSONLeaf, allowed))
}

func TestPayloadJSONRejectsMalformedBeforeCustomCallback(t *testing.T) {
	for _, raw := range []string{`{`, `1 2`, `{"n":}`, `[1,]`, `"unterminated`, `[` + strings.Repeat("0,", maxPayloadEntries) + `0]`} {
		t.Run(raw[:min(len(raw), 30)], func(t *testing.T) {
			// Arrange.
			policy := &countingPayloadPolicy{}
			tracker, _, _ := newTestTracker(t, WithPayloadPolicy(policy))
			payload := Payload{SystemInstructions: []ContentPart{{Arguments: json.RawMessage(raw)}}}
			// Act.
			err := tracker.RecordInteraction(context.Background(), testMeta(), payload, Usage{})
			// Assert: bounded category only and no policy work.
			require.ErrorIs(t, err, ErrInvalidObservation)
			assert.Zero(t, policy.calls)
			assert.Less(t, len(err.Error()), 160)
		})
	}
}

type numericInputPolicy struct{ observed json.RawMessage }

func (p *numericInputPolicy) SanitizePayload(payload Payload) Payload {
	p.observed = append(json.RawMessage(nil), payload.SystemInstructions[0].Arguments...)
	return payload
}
func (*numericInputPolicy) SanitizeToolCall(call ToolCall) ToolCall         { return call }
func (*numericInputPolicy) SanitizeToolResult(result ToolResult) ToolResult { return result }

func TestCustomSanitizerReceivesOriginalRawNumericJSON(t *testing.T) {
	// Arrange.
	raw := json.RawMessage(`{ "n":9007199254740993, "d":1.0000000000000000001e+99 }`)
	policy := &numericInputPolicy{}
	tracker, _, _ := newTestTracker(t, WithPayloadPolicy(policy))
	// Act.
	err := tracker.RecordInteraction(
		context.Background(),
		testMeta(),
		Payload{SystemInstructions: []ContentPart{{Arguments: raw}}},
		Usage{},
	)
	// Assert: prevalidation does not rewrite the caller's bytes.
	require.NoError(t, err)
	assert.Equal(t, raw, policy.observed)
}

func TestToolJSONStructuredLookingInvalidRejected(t *testing.T) {
	// Arrange.
	for _, raw := range []string{`{`, `[1,]`, `"unterminated`} {
		// Act and Assert.
		require.ErrorIs(t, validateToolJSONInput(raw), ErrInvalidObservation)
	}
	require.NoError(t, validateToolJSONInput("ordinary tool text"))
	require.NoError(t, validateToolJSONInput(`9007199254740993`))
	require.Error(t, func() error { _, err := decodePayloadJSON("1 2"); return err }())
}

type toolInputPolicy struct {
	calls    int
	observed []string
}

func (*toolInputPolicy) SanitizePayload(payload Payload) Payload { return payload }
func (p *toolInputPolicy) SanitizeToolCall(call ToolCall) ToolCall {
	p.calls++
	p.observed = append(p.observed, call.Arguments)
	return call
}
func (p *toolInputPolicy) SanitizeToolResult(result ToolResult) ToolResult {
	p.calls++
	p.observed = append(p.observed, result.Result)
	return result
}

func TestToolCustomSanitizerInputValidationAndPrecision(t *testing.T) {
	// Arrange: malformed and oversized structured input must not reach policy code.
	for _, raw := range []string{`{`, `{"n":}`, strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33), `[` + strings.Repeat("0,", maxPayloadEntries) + `0]`, strings.Repeat("x", maxPayloadInputBytes+1)} {
		policy := &toolInputPolicy{}
		tracker, _, _ := newTestTracker(t, WithPayloadPolicy(policy))
		// Act.
		_, handle, err := tracker.Runtime().StartTool(context.Background(), ToolCall{Name: "search", Arguments: raw})
		require.NoError(t, err)
		require.NoError(t, handle.Finish(ToolResult{Result: raw}))
		// Assert: rejection is observational and avoids custom sanitizer execution.
		assert.Zero(t, policy.calls)
	}
	// Arrange.
	raw := `{ "n":9007199254740993, "decimal":1.0000000000000000001e+99 }`
	policy := &toolInputPolicy{}
	tracker, _, _ := newTestTracker(t, WithPayloadPolicy(policy))
	// Act.
	_, handle, err := tracker.Runtime().StartTool(context.Background(), ToolCall{Name: "search", Arguments: raw})
	require.NoError(t, err)
	require.NoError(t, handle.Finish(ToolResult{Result: raw}))
	// Assert: both policy inputs retain original bytes.
	assert.Equal(t, []string{raw, raw}, policy.observed)
}

func TestPayloadWholeChannelBoundsBeforePolicy(t *testing.T) {
	// Arrange: each raw field fits its local bounds, while channel wrappers do not.
	messages := make([]Message, 2048)
	for i := range messages {
		messages[i].Role = "user"
	}
	nested := json.RawMessage(strings.Repeat("[", maxPayloadJSONDepth) + "0" + strings.Repeat("]", maxPayloadJSONDepth))
	invalidUTF8 := json.RawMessage{'"', 0xff, '"'}
	for _, payload := range []Payload{
		{InputMessages: messages},
		{SystemInstructions: []ContentPart{{Arguments: nested}}},
		{SystemInstructions: []ContentPart{{Arguments: invalidUTF8}}},
		{SystemInstructions: []ContentPart{{Content: strings.Repeat("<", maxPayloadInputBytes/6)}}},
	} {
		policy := &countingPayloadPolicy{}
		tracker, _, exporter := newTestTracker(t, WithPayloadPolicy(policy))
		// Act.
		err := tracker.RecordInteraction(context.Background(), testMeta(), payload, Usage{})
		// Assert: no policy or instrumentation before full-channel validation.
		require.ErrorIs(t, err, ErrInvalidObservation)
		assert.Zero(t, policy.calls)
		assert.Empty(t, exporter.GetSpans())
	}
}

func TestPayloadWholeChannelExactBoundaries(t *testing.T) {
	// Arrange: array + 1365 message objects (role and parts) total 4096 nodes.
	messages := make([]Message, (maxPayloadEntries-1)/3)
	for i := range messages {
		messages[i].Role = "user"
	}
	nested := json.RawMessage(
		strings.Repeat("[", maxPayloadJSONDepth-2) + "0" + strings.Repeat("]", maxPayloadJSONDepth-2),
	)
	emptyChannel, err := json.Marshal([]ContentPart{{Content: "x"}})
	require.NoError(t, err)
	exactContent := strings.Repeat("x", maxPayloadInputBytes-len(emptyChannel)+1)
	for _, payload := range []Payload{
		{InputMessages: messages},
		{SystemInstructions: []ContentPart{{Arguments: nested}}},
		{SystemInstructions: []ContentPart{{Content: exactContent}}},
	} {
		policy := &countingPayloadPolicy{}
		tracker, _, _ := newTestTracker(t, WithPayloadPolicy(policy))
		// Act.
		err := tracker.RecordInteraction(context.Background(), testMeta(), payload, Usage{})
		// Assert: the exact node/depth boundaries permit one callback.
		require.NoError(t, err)
		assert.Equal(t, 1, policy.calls)
	}
}
