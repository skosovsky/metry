package grpc

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/skosovsky/metry/metrytest"
)

type observedHealth struct {
	healthpb.UnimplementedHealthServer

	observed  chan trace.SpanContext
	cancelled chan struct{}
}

func (h *observedHealth) Check(
	ctx context.Context,
	req *healthpb.HealthCheckRequest,
) (*healthpb.HealthCheckResponse, error) {
	h.observed <- trace.SpanContextFromContext(ctx)
	if req.GetService() == "error" {
		return nil, status.Error(grpccodes.Unavailable, "service unavailable")
	}
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}
func (h *observedHealth) Watch(_ *healthpb.HealthCheckRequest, stream healthpb.Health_WatchServer) error {
	h.observed <- trace.SpanContextFromContext(stream.Context())
	if err := stream.Send(&healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}); err != nil {
		return err
	}
	<-stream.Context().Done()
	close(h.cancelled)
	return status.FromContextError(stream.Context().Err()).Err()
}

func TestGRPCLifecycle_UnaryErrorAndStreamingCancellation(t *testing.T) {
	// Arrange.
	provider, exporter := metrytest.NewTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "test", "caller")
	require.NoError(t, err)
	origin := trace.SpanContextFromContext(ctx)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(ServerOptions(provider)...)
	service := &observedHealth{observed: make(chan trace.SpanContext, 2), cancelled: make(chan struct{})}
	healthpb.RegisterHealthServer(server, service)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	connection, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		ClientDialOption(provider),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	client := healthpb.NewHealthClient(connection)
	// Act.
	_, err = client.Check(ctx, &healthpb.HealthCheckRequest{Service: "error"})
	require.Equal(t, grpccodes.Unavailable, status.Code(err))
	streamCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	stream, err := client.Watch(streamCtx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	message, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, message.GetStatus())
	cancel()
	_, err = stream.Recv()
	require.Equal(t, grpccodes.Canceled, status.Code(err))
	<-service.cancelled
	require.NoError(t, connection.Close())
	server.Stop()
	end()
	require.NoError(t, provider.ForceFlush(context.Background()))
	// Assert.
	for range 2 {
		serverContext := <-service.observed
		require.True(t, serverContext.IsValid())
		require.Equal(t, origin.TraceID(), serverContext.TraceID())
	}
	spans := exporter.GetSpans()
	var errorSpans int
	for _, span := range spans {
		require.Equal(t, origin.TraceID(), span.SpanContext.TraceID())
		if span.Status.Code == codes.Error {
			errorSpans++
		}
	}
	require.GreaterOrEqual(t, len(spans), 5)
	require.GreaterOrEqual(t, errorSpans, 2)
}
