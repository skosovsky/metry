package genai

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/skosovsky/metry/internal/traceutil"
)

// RecordOperation runs fn with scope attached to context inside a new genai.operation span.
func (t *Tracker) RecordOperation(
	ctx context.Context,
	scope Scope,
	fn func(context.Context) error,
) error {
	ctx = WithScope(ctx, scope)
	ctx, span := t.tracer.Start(ctx, "genai.operation")
	defer span.End()

	if attrs := scopeOperationSpanAttributes(scope); len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}

	if err := fn(ctx); err != nil {
		traceutil.SpanError(span, err)
		return err
	}
	traceutil.SpanOK(span)
	return nil
}

func scopeOperationSpanAttributes(scope Scope) []attribute.KeyValue {
	meta := Meta{ //nolint:exhaustruct // partial meta for scope attrs only
		Provider:      scope.Provider,
		Operation:     scope.Operation,
		RequestModel:  scope.Model,
		ResponseModel: scope.Model,
	}
	attrs := buildMetaAttributes(meta)
	if scope.Purpose != "" {
		attrs = append(attrs, attribute.Key(OperationPurpose).String(scope.Purpose))
	}
	return attrs
}
