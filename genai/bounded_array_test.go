package genai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundedJSONArrayWholeElementPrefix(t *testing.T) {
	input := json.RawMessage(`[{"n":9007199254740993},{"n":1e0},{"n":2}]`)
	for _, budget := range []int{18, 25, 37, 100} {
		encoded, truncated, err := boundedJSONArray(input, budget)
		require.NoError(t, err)
		require.LessOrEqual(t, len(encoded), budget)
		var parts []json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(encoded), &parts))
		var original []json.RawMessage
		require.NoError(t, json.Unmarshal(input, &original))
		assert.Equal(t, original[:len(parts)], parts)
		assert.Equal(t, len(parts) < len(original), truncated)
	}
}

func TestBoundedJSONArrayRejectsEntireInputBeforeTruncation(t *testing.T) {
	for _, input := range []json.RawMessage{
		json.RawMessage(`{"data":[],"truncated":true}`),
		json.RawMessage(`["` + strings.Repeat("x", maxPayloadInputBytes) + `"]`),
		json.RawMessage(`[` + strings.Repeat(`0,`, maxPayloadEntries) + `0]`),
	} {
		encoded, truncated, err := boundedJSONArray(input, 18)
		require.Error(t, err)
		assert.Empty(t, encoded)
		assert.False(t, truncated)
	}
}

func TestConformanceRecognizedContentVocabulary(t *testing.T) {
	contract, err := NewContract(WithRawPayloads())
	require.NoError(t, err)
	for _, value := range []string{
		`[{"type":"text","content":""}]`,
		`[{"type":"tool_call","name":"lookup","arguments":[9007199254740993]}]`,
		`[{"type":"tool_call_response","response":null}]`,
	} {
		assert.True(t, validPayloadEvidence(contract, PayloadEvidence{Attribute: SystemInstructions, Value: value}))
	}
	for _, value := range []string{
		`[{"type":"text","text":"old"}]`,
		`[{"type":"tool_call"}]`,
		`[{"type":"tool_call_response","result":true}]`,
		`{"data":[],"truncated":true}`,
	} {
		assert.False(t, validPayloadEvidence(contract, PayloadEvidence{Attribute: SystemInstructions, Value: value}))
	}
}

func TestConformanceTruncationMarkerRequiresItsArrayChannel(t *testing.T) {
	contract, err := NewContract(WithRawPayloads())
	require.NoError(t, err)
	profile := contract.Profile()
	marker, _ := declaredField(profile, payloadTruncationKey(InputMessages))
	channel, _ := declaredField(profile, InputMessages)
	assert.False(t, completePayloadEvidence(contract, []ProfileField{marker}, nil))
	assert.True(
		t,
		completePayloadEvidence(
			contract,
			[]ProfileField{marker, channel},
			[]PayloadEvidence{{Attribute: InputMessages, Value: "[]"}},
		),
	)
	marker.Encoding = EncodingString
	assert.False(
		t,
		completePayloadEvidence(
			contract,
			[]ProfileField{marker, channel},
			[]PayloadEvidence{{Attribute: InputMessages, Value: "[]"}},
		),
	)
}

func TestBoundedJSONArrayCanonicalEncodingDoesNotInventTruncation(t *testing.T) {
	// Arrange: original escape spelling exceeds the budget, but every element fits
	// after JSON decoding and re-encoding. No array element has been omitted.
	input := json.RawMessage(`["\/\/\/\/\/\/\/\/\/\/\/\/"]`)
	// Act.
	output, truncated, err := boundedJSONArray(input, 18)
	// Assert.
	require.NoError(t, err)
	assert.Equal(t, `["////////////"]`, output)
	assert.False(t, truncated)
}
