package genai

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type oversizedPayloadPolicy struct{}

func (oversizedPayloadPolicy) SanitizePayload(Payload) Payload {
	return Payload{SystemInstructions: []ContentPart{{Content: strings.Repeat("x", maxPayloadInputBytes+1)}}}
}
func (oversizedPayloadPolicy) SanitizeToolCall(call ToolCall) ToolCall         { return call }
func (oversizedPayloadPolicy) SanitizeToolResult(result ToolResult) ToolResult { return result }

func TestCustomPolicyOutputRejectedBeforeEncodingAndWrites(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t, WithPayloadPolicy(oversizedPayloadPolicy{}))
	// Act.
	err := tracker.RecordInteraction(context.Background(), testMeta(), Payload{}, Usage{})
	// Assert: policy output cannot grow the library's encoding/input budget.
	require.ErrorIs(t, err, ErrInvalidObservation)
	flushTestProvider(t, provider)
	require.Empty(t, exporter.GetSpans())
}
