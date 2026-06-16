package genai

import (
	"context"

	"github.com/skosovsky/metry"
)

const (
	scopeBaggageProvider  = metry.GenAIBaggageProviderKey
	scopeBaggageModel     = metry.GenAIBaggageModelKey
	scopeBaggageOperation = metry.GenAIBaggageOperationKey
	scopeBaggagePurpose   = metry.GenAIBaggagePurposeKey
)

// WithScope attaches a typed GenAI scope to context via observability baggage.
func WithScope(ctx context.Context, scope Scope) context.Context {
	attrs := scopeAttributes(scope)
	if len(attrs) == 0 {
		return ctx
	}
	return metry.Enrich(ctx, attrs...)
}

// WithScope is a Tracker sugar wrapper around the package-level WithScope.
func (t *Tracker) WithScope(ctx context.Context, scope Scope) context.Context {
	_ = t
	return WithScope(ctx, scope)
}

// ScopeFromContext returns the GenAI scope stored in baggage, if any.
func ScopeFromContext(ctx context.Context) (Scope, bool) {
	var scope Scope
	var found bool

	if v := metry.BaggageMember(ctx, scopeBaggageProvider); v != "" {
		scope.Provider = v
		found = true
	}
	if v := metry.BaggageMember(ctx, scopeBaggageModel); v != "" {
		scope.Model = v
		found = true
	}
	if v := metry.BaggageMember(ctx, scopeBaggageOperation); v != "" {
		scope.Operation = v
		found = true
	}
	if v := metry.BaggageMember(ctx, scopeBaggagePurpose); v != "" {
		scope.Purpose = v
		found = true
	}
	return scope, found
}

func scopeAttributes(scope Scope) []metry.Attribute {
	var attrs []metry.Attribute
	if scope.Provider != "" {
		attrs = append(attrs, scopeProviderAttribute(scope.Provider))
	}
	if scope.Model != "" {
		attrs = append(attrs, scopeModelAttribute(scope.Model))
	}
	if scope.Operation != "" {
		attrs = append(attrs, scopeOperationAttribute(scope.Operation))
	}
	if scope.Purpose != "" {
		attrs = append(attrs, scopePurposeAttribute(scope.Purpose))
	}
	return attrs
}

func scopeProviderAttribute(provider string) metry.Attribute {
	return metry.GenAIProvider(provider)
}

func scopeModelAttribute(model string) metry.Attribute {
	return metry.GenAIModel(model)
}

func scopeOperationAttribute(operation string) metry.Attribute {
	return metry.GenAIOperation(operation)
}

func scopePurposeAttribute(purpose string) metry.Attribute {
	return metry.GenAIPurpose(purpose)
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
	if meta.ResponseModel == "" {
		meta.ResponseModel = scope.Model
	}
	if meta.Purpose == "" {
		meta.Purpose = scope.Purpose
	}
	return meta
}
