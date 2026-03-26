package grpc

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"

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

func TestServerWithOptions_StartsAndStops(t *testing.T) {
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

func TestHandlers_NilDependency_Panics(t *testing.T) {
	require.Panics(t, func() { _ = ServerStatsHandler(nil) })
	require.Panics(t, func() { _ = ClientStatsHandler(nil) })
}

func TestHandlers_IncompleteProvider_Panics(t *testing.T) {
	provider := &metry.Provider{}

	require.Panics(t, func() { _ = ServerStatsHandler(provider) })
	require.Panics(t, func() { _ = ClientStatsHandler(provider) })
}

func TestClientServerHandlers_PropagateTraceAndExportSpans(t *testing.T) {
	ctx := context.Background()
	traceExporter := tracetest.NewInMemoryExporter()
	provider, err := metry.New(
		ctx,
		metry.WithServiceName("test-grpc-propagation"),
		metry.WithExporter(traceExporter),
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
	tpSDK, ok := provider.TracerProvider.(*sdktrace.TracerProvider)
	require.True(t, ok)
	require.NoError(t, tpSDK.ForceFlush(ctx))
	spans := traceExporter.GetSpans()
	require.GreaterOrEqual(t, len(spans), 2)
	assert.True(t, spansContainParentChildRelation(spans))
}

func spansContainParentChildRelation(spans tracetest.SpanStubs) bool {
	for i := range spans {
		if !spans[i].Parent.SpanID().IsValid() {
			continue
		}
		for j := range spans {
			if i == j {
				continue
			}
			if spans[i].SpanContext.TraceID() != spans[j].SpanContext.TraceID() {
				continue
			}
			if spans[i].Parent.SpanID() == spans[j].SpanContext.SpanID() {
				return true
			}
		}
	}
	return false
}
