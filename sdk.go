package metry

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/skosovsky/metry/diagnostic"
	metryprop "github.com/skosovsky/metry/propagation"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var (
	// ErrInvalidConfiguration identifies invalid configuration before resource ownership transfers.
	ErrInvalidConfiguration = errors.New("metry: invalid configuration")
	// ErrInvalidSDKDependencies identifies missing SDK dependencies, including typed nils.
	ErrInvalidSDKDependencies = errors.New("metry: invalid SDK dependencies")
)

// SDKDependencies is the composition-root boundary for borrowed OTel dependencies.
// All fields are required by FromSDK. The host owns their lifetime.
type SDKDependencies struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
}

// FromSDK borrows dependencies without installing globals or starting workers.
// Shutdown and ForceFlush never close or flush these borrowed dependencies.
func FromSDK(deps SDKDependencies, opts ...SDKOption) (*Provider, error) {
	if nilDependency(deps.TracerProvider) || nilDependency(deps.MeterProvider) || nilDependency(deps.Propagator) {
		return nil, ErrInvalidSDKDependencies
	}
	var cfg sdkConfig
	for _, opt := range opts {
		if opt == nil {
			return nil, ErrInvalidConfiguration
		}
		opt(&cfg)
	}
	return &Provider{ //nolint:exhaustruct_v5 // borrowed providers have no owned SDK resources.
		otelTracer:        deps.TracerProvider,
		otelMeter:         deps.MeterProvider,
		propagator:        metryprop.Wrap(deps.Propagator, cfg.propagationPolicy),
		propagationPolicy: cfg.propagationPolicy,
		diagnosticPolicy:  cfg.diagnosticPolicy,
	}, nil
}

// SDK exposes provider references and a policy-wrapped propagator for transport adapters.
// Nil/zero providers expose noop providers and trace-only propagation.
func (p *Provider) SDK() SDKDependencies {
	return SDKDependencies{
		TracerProvider: p.tracerProvider(),
		MeterProvider:  p.meterProvider(),
		Propagator:     p.textMapPropagator(),
	}
}

// FlushDependencies explicitly flushes owned or borrowed providers that support ForceFlush.
// Unlike ForceFlush, this operation is allowed to affect host-owned pipelines.
func (p *Provider) FlushDependencies(ctx context.Context) error {
	if p == nil {
		return nil
	}
	var errs []error
	for _, dep := range []struct {
		name  string
		value any
	}{
		{name: "tracer", value: p.otelTracer},
		{name: "meter", value: p.otelMeter},
	} {
		if flusher, ok := dep.value.(interface{ ForceFlush(context.Context) error }); ok {
			if err := flusher.ForceFlush(ctx); err != nil {
				errs = append(errs, fmt.Errorf("%s force flush: %w", dep.name, err))
			}
		}
	}
	return errors.Join(errs...)
}

// nilDependency checks interface-wrapped nils before invoking user dependencies.
func nilDependency(dep any) bool {
	if dep == nil {
		return true
	}
	v := reflect.ValueOf(dep)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// newProviderFromDeps is a test-only partial provider: it deliberately permits nil dependencies.
func newProviderFromDeps(
	tracer trace.TracerProvider,
	meter metric.MeterProvider,
	prop propagation.TextMapPropagator,
) *Provider {
	if prop != nil {
		prop = metryprop.Wrap(prop, metryprop.Policy{})
	}
	return &Provider{ //nolint:exhaustruct_v5 // partial dependency tests deliberately have no owned resources.
		otelTracer: tracer,
		otelMeter:  meter,
		propagator: prop,
	}
}

// SDKOption configures the metry boundary around borrowed OTel dependencies.
type SDKOption func(*sdkConfig)

type sdkConfig struct {
	propagationPolicy metryprop.Policy
	diagnosticPolicy  diagnostic.Policy
}

// WithSDKPropagationPolicy sets ingress/egress policy without changing the host propagator.
func WithSDKPropagationPolicy(policy metryprop.Policy) SDKOption {
	return func(c *sdkConfig) { c.propagationPolicy = policy }
}

// WithSDKDiagnosticPolicy sets the policy for metry-owned text on borrowed providers.
func WithSDKDiagnosticPolicy(policy diagnostic.Policy) SDKOption {
	return func(c *sdkConfig) { c.diagnosticPolicy = policy }
}
