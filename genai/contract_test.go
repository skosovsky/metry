package genai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractRejectsExplicitInvalidBudgets(t *testing.T) {
	for _, option := range []Option{WithMaxContextLength(0), WithMaxContextLength(len(truncatedJSONMarker) - 1), WithMaxContextLength(maxPayloadInputBytes + 1), WithMaxEventLength(0), WithMaxEventLength(-1), WithMaxEventLength(maxPayloadInputBytes + 1)} {
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
		WithMaxContextLength(100000),
		WithMaxEventLength(17),
	)
	// Act.
	contract := tracker.Contract()
	copied := contract.Profile()
	copied.Fields[0].Name = "changed"
	copied.Dimensions[1].Values[0] = "changed"
	channels := contract.PayloadChannels()
	channels[0].OutputByteLimit = 1
	profile.Fields[0].Name = "changed"
	// Assert.
	require.NoError(t, contract.Validate())
	require.True(t, contract.RawEnabled())
	require.Equal(t, InstrumentationProfileID, contract.Profile().ID)
	require.NotEqual(t, "changed", contract.Profile().Fields[0].Name)
	require.NotEqual(t, "changed", contract.Profile().Dimensions[1].Values[0])
	for _, channel := range contract.PayloadChannels() {
		expected := 100000
		if channel.Attribute == EvaluationExplanation {
			expected = 17
		}
		require.Equal(t, expected, channel.OutputByteLimit)
	}
}
