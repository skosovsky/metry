// Package grpc provides gRPC instrumentation for metry: server and client stats handlers
// that create spans and propagate trace context. Use after metry.Init.
package grpc

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

// ServerStatsHandler returns a gRPC stats.Handler for server-side tracing and metrics.
// Use with grpc.NewServer(grpc.StatsHandler(metrygrpc.ServerStatsHandler())).
// Covers both unary and streaming RPCs.
func ServerStatsHandler() stats.Handler {
	return otelgrpc.NewServerHandler()
}

// ClientStatsHandler returns a gRPC stats.Handler for client-side tracing and propagation.
// Use with grpc.NewClient(addr, grpc.WithStatsHandler(metrygrpc.ClientStatsHandler()), ...).
// Covers both unary and streaming RPCs.
func ClientStatsHandler() stats.Handler {
	return otelgrpc.NewClientHandler()
}

// ServerOptions returns gRPC server options that install the OTel stats handler.
// Use with grpc.NewServer(metrygrpc.ServerOptions()...).
func ServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{grpc.StatsHandler(ServerStatsHandler())}
}

// ClientDialOption returns a gRPC DialOption that installs the OTel stats handler for the client.
// Use with grpc.NewClient(addr, metrygrpc.ClientDialOption(), ...).
func ClientDialOption() grpc.DialOption {
	return grpc.WithStatsHandler(ClientStatsHandler())
}
