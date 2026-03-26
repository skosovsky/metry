// Package grpc provides gRPC instrumentation for metry: server and client stats handlers
// that create spans and propagate trace context.
package grpc

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"

	"github.com/skosovsky/metry"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

// ServerStatsHandler returns a gRPC stats.Handler for server-side tracing and metrics.
// Use with grpc.NewServer(grpc.StatsHandler(
//
//	metrygrpc.ServerStatsHandler(provider),
//
// )).
// Covers both unary and streaming RPCs.
func ServerStatsHandler(provider *metry.Provider) stats.Handler {
	if provider == nil {
		panic("metry/grpc: provider is required")
	}
	if provider.TracerProvider == nil {
		panic("metry/grpc: tracer provider is required")
	}
	if provider.MeterProvider == nil {
		panic("metry/grpc: meter provider is required")
	}
	if provider.Propagator == nil {
		panic("metry/grpc: propagator is required")
	}

	return otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(provider.TracerProvider),
		otelgrpc.WithMeterProvider(provider.MeterProvider),
		otelgrpc.WithPropagators(provider.Propagator),
	)
}

// ClientStatsHandler returns a gRPC stats.Handler for client-side tracing and propagation.
// Use with grpc.NewClient(addr, grpc.WithStatsHandler(
//
//	metrygrpc.ClientStatsHandler(provider),
//
// ), ...).
// Covers both unary and streaming RPCs.
func ClientStatsHandler(provider *metry.Provider) stats.Handler {
	if provider == nil {
		panic("metry/grpc: provider is required")
	}
	if provider.TracerProvider == nil {
		panic("metry/grpc: tracer provider is required")
	}
	if provider.MeterProvider == nil {
		panic("metry/grpc: meter provider is required")
	}
	if provider.Propagator == nil {
		panic("metry/grpc: propagator is required")
	}

	return otelgrpc.NewClientHandler(
		otelgrpc.WithTracerProvider(provider.TracerProvider),
		otelgrpc.WithMeterProvider(provider.MeterProvider),
		otelgrpc.WithPropagators(provider.Propagator),
	)
}

// ServerOptions returns gRPC server options that install the OTel stats handler.
// Use with grpc.NewServer(metrygrpc.ServerOptions(provider)...).
func ServerOptions(provider *metry.Provider) []grpc.ServerOption {
	return []grpc.ServerOption{grpc.StatsHandler(ServerStatsHandler(provider))}
}

// ClientDialOption returns a gRPC DialOption that installs the OTel stats handler for the client.
// Use with grpc.NewClient(addr, metrygrpc.ClientDialOption(provider), ...).
func ClientDialOption(provider *metry.Provider) grpc.DialOption {
	return grpc.WithStatsHandler(ClientStatsHandler(provider))
}
