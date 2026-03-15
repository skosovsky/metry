package metry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetBaggageValue_InjectsValue(t *testing.T) {
	ctx := context.Background()
	ctx, err := SetBaggageValue(ctx, "patient_id", "p-123")
	require.NoError(t, err)
	assert.Equal(t, "p-123", BaggageValue(ctx, "patient_id"))
}

func TestSetBaggageValue_InvalidKey_ReturnsError(t *testing.T) {
	ctx := context.Background()
	_, err := SetBaggageValue(ctx, "invalid key /", "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metry:")
	assert.Contains(t, err.Error(), "W3C")
}

func TestBaggageValue_MissingKey_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	assert.Empty(t, BaggageValue(ctx, "nonexistent"))
}
