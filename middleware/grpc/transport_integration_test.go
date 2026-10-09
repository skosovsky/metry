//go:build integration

package grpc

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
	"github.com/skosovsky/metry/testutil"
)

func TestIntegrationServerWithOptions_StartsAndStops(t *testing.T) {
	ctx := context.Background()
	provider, err := metry.New(ctx, metry.WithServiceName("test-grpc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })

	srv := grpc.NewServer(ServerOptions(provider)...)
	go func() { _ = srv.Serve(lis) }()
	srv.Stop()
}

func TestIntegrationClientServerHandlers_PropagateTraceAndExportSpans(t *testing.T) {
	ctx := context.Background()
	traceExporter := testutil.NewInMemoryTraceExporter()
	provider, err := metry.New(
		ctx,
		metry.WithServiceName("test-grpc-propagation"),
		metry.WithExporter(traceExporter.SDKSpanExporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })

	srv := grpc.NewServer(ServerOptions(provider)...)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(srv, healthServer)
	t.Cleanup(func() { srv.Stop() })

	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		ClientDialOption(provider),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	client := grpc_health_v1.NewHealthClient(conn)
	_, err = client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)

	require.NoError(t, conn.Close())
	srv.Stop()

	// Batched spans are exported on TracerProvider shutdown, but tracetest.InMemoryExporter.Shutdown
	// clears its buffer — so read spans after ForceFlush, before Provider.Shutdown (t.Cleanup).
	require.NoError(t, provider.ForceFlush(ctx))
	spans := traceExporter.GetSpans()
	require.GreaterOrEqual(t, len(spans), 2)
	assert.True(t, testutil.SpansContainParentChildRelation(spans))
}
