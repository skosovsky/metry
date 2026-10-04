package genai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicContractShapesCompile(t *testing.T) {
	// Arrange: assignments are compile-time assertions for the public boundaries.
	var _ Runtime = trackerRuntime{tracker: nil, provider: ""}
	var _ Runtime = noopRuntime{}
	var diagnostic error = &ObservationError{Field: "usage", Rule: "invalid"}
	var _ ProviderAdapter = fakeProviderAdapter{}
	var _ PayloadPolicy = rawPayloadPolicy{}
	var _ PayloadPolicy = redactPayloadPolicy{keys: nil}
	var _ PayloadPolicy = hashPayloadPolicy{}
	assertMeasurementConstructors(Observe[int], Observe[float64])
	// Act.
	measurement := Observe(0, UnitTokens, SourceMeasured)
	// Assert.
	assert.True(t, measurement.Present)
	assert.Equal(t, 0, measurement.Value)
	require.ErrorIs(t, diagnostic, ErrInvalidObservation)
}

func assertMeasurementConstructors(
	_ func(int, Unit, Source) Measurement[int],
	_ func(float64, Unit, Source) Measurement[float64],
) {
}
