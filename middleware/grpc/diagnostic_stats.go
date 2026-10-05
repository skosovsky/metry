package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"github.com/skosovsky/metry/diagnostic"
)

// diagnosticStats copies completion evidence before handing it to instrumentation.
// The RPC and the host's original stats event retain their original error.
type diagnosticStats struct {
	stats.Handler

	policy diagnostic.Policy
}

func (h diagnosticStats) HandleRPC(ctx context.Context, event stats.RPCStats) {
	end, ok := event.(*stats.End)
	if !ok || end == nil || end.Error == nil {
		h.Handler.HandleRPC(ctx, event)
		return
	}
	result := h.policy.Error(end.Error)
	safe := *end
	safe.Error = status.Error(rpcErrorCode(end.Error), result.Message)
	h.Handler.HandleRPC(ctx, &safe)
}

// rpcErrorCode asks only for the machine code. status.FromError on a wrapped
// status calls the original Error method to construct a description.
//
//nolint:nonamedreturns // recovered host callbacks must return the safe code.
func rpcErrorCode(err error) (code codes.Code) {
	code = codes.Unknown
	defer func() {
		if recover() != nil {
			code = codes.Unknown
		}
	}()
	var coded interface{ GRPCStatus() *status.Status }
	if errors.As(err, &coded) {
		if result := coded.GRPCStatus(); result != nil {
			code = result.Code()
		}
	}
	return code
}
