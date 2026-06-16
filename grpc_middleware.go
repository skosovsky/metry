package metry

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

// GRPCServerStatsHandler returns a gRPC stats.Handler for server-side tracing and metrics.
func GRPCServerStatsHandler(provider *Provider) stats.Handler {
	if provider == nil {
		panic("metry: provider is required")
	}
	return otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(provider.tracerProvider()),
		otelgrpc.WithMeterProvider(provider.meterProvider()),
		otelgrpc.WithPropagators(provider.textMapPropagator()),
	)
}

// GRPCClientStatsHandler returns a gRPC stats.Handler for client-side tracing and propagation.
func GRPCClientStatsHandler(provider *Provider) stats.Handler {
	if provider == nil {
		panic("metry: provider is required")
	}
	return otelgrpc.NewClientHandler(
		otelgrpc.WithTracerProvider(provider.tracerProvider()),
		otelgrpc.WithMeterProvider(provider.meterProvider()),
		otelgrpc.WithPropagators(provider.textMapPropagator()),
	)
}

// GRPCServerOptions returns gRPC server options that install the OTel stats handler.
func GRPCServerOptions(provider *Provider) []grpc.ServerOption {
	return []grpc.ServerOption{grpc.StatsHandler(GRPCServerStatsHandler(provider))}
}

// GRPCClientDialOption returns a gRPC DialOption that installs the OTel stats handler for the client.
func GRPCClientDialOption(provider *Provider) grpc.DialOption {
	return grpc.WithStatsHandler(GRPCClientStatsHandler(provider))
}
