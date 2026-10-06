package genai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractRejectsExplicitInvalidBudgets(t *testing.T) {
	for _, option := range []Option{WithPayloadByteLimit(0), WithPayloadByteLimit(len(truncatedJSONMarker) - 1), WithPayloadByteLimit(maxPayloadInputBytes + 1), WithEventTextByteLimit(0), WithEventTextByteLimit(-1), WithEventTextByteLimit(maxPayloadInputBytes + 1)} {
		// Arrange.
		options := []Option{option}
		// Act.
		_, err := NewContract(options...)
		// Assert.
		require.ErrorIs(t, err, ErrInvalidObservation)
	}
	require.ErrorIs(t, (Contract{}).Validate(), ErrInvalidObservation)
}

func TestContractSnapshotIsDetachedAndResolved(t *testing.T) {
	// Arrange.
	profile := DefaultProfile()
	tracker, _, _, _ := newTestTrackerWithMetrics(
		t,
		WithProfile(profile),
		WithRawPayloads(),
		WithPayloadByteLimit(100000),
		WithEventTextByteLimit(17),
	)
	// Act.
	contract := tracker.Contract()
	copied := contract.Profile()
	fields := contract.Schema().Fields()
	fields[0].Name = "changed"
	copied.Dimensions[1].Values[0] = "changed"
	channels := contract.PayloadChannels()
	channels[0].OutputByteLimit = 1
	// Assert.
	require.NoError(t, contract.Validate())
	require.True(t, contract.RawEnabled())
	require.Equal(t, InstrumentationProfileID, contract.Schema().ID())
	require.NotEqual(t, "changed", contract.Schema().Fields()[0].Name)
	require.NotEqual(t, "changed", contract.Profile().Dimensions[1].Values[0])
	for _, channel := range contract.PayloadChannels() {
		expected := 100000
		if channel.Attribute == EvaluationExplanation {
			expected = 17
		}
		require.Equal(t, expected, channel.OutputByteLimit)
	}
}
