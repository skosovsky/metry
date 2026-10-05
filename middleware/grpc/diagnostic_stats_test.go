package grpc_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/diagnostic"
	mgrpc "github.com/skosovsky/metry/middleware/grpc"
)

const rpcSecret = "SENTINEL_SECRET"

type countedRPCError struct{ calls int }

func (e *countedRPCError) Error() string            { e.calls++; return rpcSecret }
func (*countedRPCError) GRPCStatus() *status.Status { return status.New(codes.Internal, rpcSecret) }

func TestRPCDiagnosticFanout(t *testing.T) {
	for _, client := range []bool{false, true} {
		for _, mode := range []string{"default", "sanitized", "raw", "failure", "panic", "small"} {
			t.Run(
				fmt.Sprintf("client=%t/%s", client, mode),
				func(t *testing.T) { checkRPCDiagnosticCase(t, client, mode) },
			)
		}
	}
}

func checkRPCDiagnosticCase(t *testing.T, client bool, mode string) {
	t.Helper()
	// Arrange: two SDK exporters receive processed completion evidence.
	policy, err := diagnostic.NewPolicy(rpcPolicyConfig(mode))
	require.NoError(t, err)
	first, second := tracetest.NewInMemoryExporter(), tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(first), sdktrace.WithSyncer(second))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	provider, err := metry.FromSDK(
		metry.SDKDependencies{
			TracerProvider: tp,
			MeterProvider:  noop.NewMeterProvider(),
			Propagator:     propagation.TraceContext{},
		},
		metry.WithSDKDiagnosticPolicy(policy),
	)
	require.NoError(t, err)
	handler := mgrpc.ServerStatsHandler(provider)
	if client {
		handler = mgrpc.ClientStatsHandler(provider)
	}
	original := &countedRPCError{}
	wrapped := fmt.Errorf("wrapped: %w", original)
	original.calls = 0
	ctx := handler.TagRPC(context.Background(), &stats.RPCTagInfo{FullMethodName: "/test.Service/Get"})
	begin := time.Now()
	handler.HandleRPC(ctx, &stats.Begin{BeginTime: begin})
	end := &stats.End{BeginTime: begin, EndTime: begin.Add(time.Millisecond), Error: wrapped}
	// Act.
	handler.HandleRPC(ctx, end)
	// Assert: caller event and error identity remain unchanged.
	require.Same(t, wrapped, end.Error)
	if mode == "default" {
		require.Zero(t, original.calls)
	}
	for _, sink := range []*tracetest.InMemoryExporter{first, second} {
		assertRPCDiagnosticSpan(t, mode, sink)
	}
}

func assertRPCDiagnosticSpan(t *testing.T, mode string, sink *tracetest.InMemoryExporter) {
	t.Helper()
	spans := sink.GetSpans()
	require.Len(t, spans, 1)
	description := spans[0].Status.Description
	if mode == "raw" {
		require.Contains(t, description, rpcSecret)
	} else {
		require.NotContains(t, description, rpcSecret)
	}
	if mode == "small" {
		require.LessOrEqual(t, len(description), 1)
	}
	require.Equal(t, "Error", spans[0].Status.Code.String())
	found := false
	for _, attr := range spans[0].Attributes {
		if strings.Contains(string(attr.Key), "status_code") {
			found = true
			require.True(t, attr.Value.AsInt64() == int64(codes.Internal) || attr.Value.AsString() == "INTERNAL")
		}
	}
	require.True(t, found, "RPC machine status code remains available")
	require.NotContains(t, fmt.Sprint(spans[0].Events), rpcSecret)
}

func rpcPolicyConfig(mode string) diagnostic.Config {
	var cfg diagnostic.Config
	switch mode {
	case "raw":
		cfg.Raw = true
	case "sanitized":
		cfg.Sanitizer = func(diagnostic.Channel, string) (string, error) { return "redacted", nil }
	case "failure":
		cfg.Sanitizer = func(diagnostic.Channel, string) (string, error) { return "", fmt.Errorf("%s", rpcSecret) }
	case "panic":
		cfg.Sanitizer = func(diagnostic.Channel, string) (string, error) { panic(rpcSecret) }
	case "small":
		cfg.Raw = true
		cfg.MaxOutputBytes = 1
	}
	return cfg
}
