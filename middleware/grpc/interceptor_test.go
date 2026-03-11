package grpc

import (
	"net"
	"testing"

	"github.com/skosovsky/metry/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestServerStatsHandler_AndClientDialOption_NonNil(t *testing.T) {
	assert.NotNil(t, ServerStatsHandler())
	assert.NotNil(t, ClientStatsHandler())
	opts := ServerOptions()
	require.Len(t, opts, 1)
	dialOpt := ClientDialOption()
	assert.NotNil(t, dialOpt)
}

func TestServerWithOptions_StartsAndStops(t *testing.T) {
	_ = testutil.SetupTestTracing(t)

	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })

	srv := grpc.NewServer(ServerOptions()...)
	go func() { _ = srv.Serve(lis) }()
	srv.Stop()
}
