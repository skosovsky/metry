package genai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTruncationEnvelopePreservesSupportedDepthBudget(t *testing.T) {
	// Arrange: adding an envelope to the retained first member would exceed depth.
	raw := "[" + strings.Repeat(
		"[",
		maxPayloadJSONDepth-1,
	) + "9007199254740993" + strings.Repeat(
		"]",
		maxPayloadJSONDepth-1,
	) + `,"` + strings.Repeat(
		"x",
		500,
	) + `"]`
	require.True(t, boundedJSONDepth(raw))
	// Act.
	encoded, err := boundedJSON(json.RawMessage(raw), 128)
	// Assert: an omitted deep member is safer than an unsupported output envelope.
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), 128)
	require.True(t, json.Valid([]byte(encoded)))
	require.True(t, boundedJSONDepth(encoded))
	require.Contains(t, encoded, `"truncated":true`)
}

func TestTruncationEnvelopePreservesNodeBudget(t *testing.T) {
	// Arrange: removing one large scalar saves bytes but only one JSON node.
	raw := "[" + strings.Repeat("1,", maxPayloadEntries-2) + `"` + strings.Repeat("x", 500) + `"]`
	_, err := decodePayloadJSON(raw)
	require.NoError(t, err)
	// Act.
	encoded, err := boundedJSON(json.RawMessage(raw), 2*maxPayloadEntries+100)
	// Assert: envelope nodes count toward the supported structural bound too.
	require.NoError(t, err)
	_, err = decodePayloadJSON(encoded)
	require.NoError(t, err)
	require.Contains(t, encoded, `"truncated":true`)
}
