package genai

import (
	"context"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/enrichment"
)

const (
	scopeBaggageProvider  = metry.GenAIBaggageProviderKey
	scopeBaggageModel     = metry.GenAIBaggageModelKey
	scopeBaggageOperation = metry.GenAIBaggageOperationKey
	scopeBaggagePurpose   = metry.GenAIBaggagePurposeKey
)

type scopeContextKey struct{}

// WithScope stores original local defaults privately, without writing telemetry
// or baggage. Non-empty fields replace previous local defaults; empty fields keep
// them. Tracker policy is applied only when a tracker emits these values.
func WithScope(ctx context.Context, scope Scope) context.Context {
	if scope == (Scope{}) { //nolint:exhaustruct_v5 // Compare all fields with the empty sentinel.
		return ctx
	}
	previous, _ := ctx.Value(scopeContextKey{}).(Scope)
	return context.WithValue(ctx, scopeContextKey{}, mergeScope(scope, previous))
}

// WithScope stores the same private defaults as package WithScope. It does not
// export through this receiver; each emitting tracker applies its own policy.
func (t *Tracker) WithScope(ctx context.Context, scope Scope) context.Context {
	return WithScope(ctx, scope)
}

// ScopeFromContext returns original private local defaults over locally trusted
// or explicitly admitted enrichment fallback. Arbitrary OTel baggage is ignored.
func ScopeFromContext(ctx context.Context) (Scope, bool) {
	scope, _ := ctx.Value(scopeContextKey{}).(Scope)
	if scope.Provider == "" {
		scope.Provider = enrichment.String(ctx, scopeBaggageProvider)
	}
	if scope.Model == "" {
		scope.Model = enrichment.String(ctx, scopeBaggageModel)
	}
	if scope.Operation == "" {
		scope.Operation = enrichment.String(ctx, scopeBaggageOperation)
	}
	if scope.Purpose == "" {
		scope.Purpose = enrichment.String(ctx, scopeBaggagePurpose)
	}
	return scope, scope != (Scope{}) //nolint:exhaustruct_v5 // Empty sentinel means no defaults.
}

func mergeScope(scope, defaults Scope) Scope {
	if scope.Provider == "" {
		scope.Provider = defaults.Provider
	}
	if scope.Model == "" {
		scope.Model = defaults.Model
	}
	if scope.Operation == "" {
		scope.Operation = defaults.Operation
	}
	if scope.Purpose == "" {
		scope.Purpose = defaults.Purpose
	}
	return scope
}

func mergeMetaFromScope(ctx context.Context, meta Meta) Meta {
	scope, ok := ScopeFromContext(ctx)
	return mergeMetaFromScopeWithScope(meta, scope, ok)
}

func mergeMetaFromScopeWithScope(meta Meta, scope Scope, ok bool) Meta {
	if !ok {
		return meta
	}
	if meta.Provider == "" {
		meta.Provider = scope.Provider
	}
	if meta.Operation == "" {
		meta.Operation = scope.Operation
	}
	if meta.RequestModel == "" {
		meta.RequestModel = scope.Model
	}
	if meta.Purpose == "" {
		meta.Purpose = scope.Purpose
	}
	return meta
}
