package metry_test

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
)

func TestGRPCServerStatsHandler_NonNil(t *testing.T) {
	ctx := context.Background()
	provider, err := metry.New(ctx, metry.WithServiceName("root-grpc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	assert.NotNil(t, metry.GRPCServerStatsHandler(provider))
	assert.NotNil(t, metry.GRPCClientStatsHandler(provider))
	opts := metry.GRPCServerOptions(provider)
	require.Len(t, opts, 1)
	assert.NotNil(t, metry.GRPCClientDialOption(provider))
}

func TestGRPCRootHandlers_PropagateTraceAndExportSpans(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("root-grpc-e2e"))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })

	srv := grpc.NewServer(metry.GRPCServerOptions(provider)...)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(srv, healthServer)
	t.Cleanup(func() { srv.Stop() })

	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		metry.GRPCClientDialOption(provider),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	client := grpc_health_v1.NewHealthClient(conn)
	_, err = client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)

	// Read spans after ForceFlush, before Provider.Shutdown (shutdown clears in-memory exporter buffer).
	require.NoError(t, provider.ForceFlush(ctx))
	require.GreaterOrEqual(t, mem.Len(), 2)
}
