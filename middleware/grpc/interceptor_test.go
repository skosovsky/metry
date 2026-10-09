package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
)

func TestServerStatsHandler_AndClientDialOption_NonNil(t *testing.T) {
	ctx := context.Background()
	provider, err := metry.New(ctx, metry.WithServiceName("test-grpc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	assert.NotNil(t, ServerStatsHandler(provider))
	assert.NotNil(t, ClientStatsHandler(provider))
	opts := ServerOptions(provider)
	require.Len(t, opts, 1)
	dialOpt := ClientDialOption(provider)
	assert.NotNil(t, dialOpt)
}

func TestHandlers_NilDependency_Panics(t *testing.T) {
	require.Panics(t, func() { _ = ServerStatsHandler(nil) })
	require.Panics(t, func() { _ = ClientStatsHandler(nil) })
}

func TestHandlers_IncompleteProvider_UsesNoopDeps(t *testing.T) {
	provider := &metry.Provider{}

	assert.NotNil(t, ServerStatsHandler(provider))
	assert.NotNil(t, ClientStatsHandler(provider))
}
