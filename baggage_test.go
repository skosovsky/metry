package metry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextWithBaggage_InjectsValue(t *testing.T) {
	ctx := context.Background()
	ctx, err := ContextWithBaggage(ctx, "patient_id", "p-123")
	require.NoError(t, err)
	assert.Equal(t, "p-123", BaggageValue(ctx, "patient_id"))
}

func TestContextWithBaggage_InvalidKey_ReturnsError(t *testing.T) {
	ctx := context.Background()
	_, err := ContextWithBaggage(ctx, "invalid key /", "value")
	require.Error(t, err)
}

func TestBaggageValue_MissingKey_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	assert.Empty(t, BaggageValue(ctx, "nonexistent"))
}
