// Package grpc provides gRPC instrumentation for metry: server and client stats handlers
// that create spans and propagate trace context.
package grpc

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"

	"github.com/skosovsky/metry"
)

// ServerStatsHandler returns a gRPC stats.Handler for server-side tracing and metrics.
func ServerStatsHandler(provider *metry.Provider) stats.Handler {
	return metry.GRPCServerStatsHandler(provider)
}

// ClientStatsHandler returns a gRPC stats.Handler for client-side tracing and propagation.
func ClientStatsHandler(provider *metry.Provider) stats.Handler {
	return metry.GRPCClientStatsHandler(provider)
}

// ServerOptions returns gRPC server options that install the OTel stats handler.
func ServerOptions(provider *metry.Provider) []grpc.ServerOption {
	return metry.GRPCServerOptions(provider)
}

// ClientDialOption returns a gRPC DialOption that installs the OTel stats handler for the client.
func ClientDialOption(provider *metry.Provider) grpc.DialOption {
	return metry.GRPCClientDialOption(provider)
}
