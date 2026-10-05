// Package grpc instruments gRPC through provider-local OTel dependencies.
package grpc

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"

	"github.com/skosovsky/metry"
)

// ServerStatsHandler returns server-side instrumentation.
func ServerStatsHandler(provider *metry.Provider) stats.Handler {
	if provider == nil {
		panic("metry: provider is required")
	}
	deps := provider.SDK()
	return diagnosticStats{Handler: otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(deps.TracerProvider),
		otelgrpc.WithMeterProvider(deps.MeterProvider),
		otelgrpc.WithPropagators(deps.Propagator),
	), policy: provider.DiagnosticPolicy()}
}

// ClientStatsHandler returns client-side instrumentation.
func ClientStatsHandler(provider *metry.Provider) stats.Handler {
	if provider == nil {
		panic("metry: provider is required")
	}
	deps := provider.SDK()
	return diagnosticStats{Handler: otelgrpc.NewClientHandler(
		otelgrpc.WithTracerProvider(deps.TracerProvider),
		otelgrpc.WithMeterProvider(deps.MeterProvider),
		otelgrpc.WithPropagators(deps.Propagator),
	), policy: provider.DiagnosticPolicy()}
}

// ServerOptions installs the OTel stats handler.
func ServerOptions(provider *metry.Provider) []grpc.ServerOption {
	return []grpc.ServerOption{grpc.StatsHandler(ServerStatsHandler(provider))}
}

// ClientDialOption installs the client stats handler.
func ClientDialOption(provider *metry.Provider) grpc.DialOption {
	return grpc.WithStatsHandler(ClientStatsHandler(provider))
}
