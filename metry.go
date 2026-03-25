package metry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/skosovsky/metry/genai"
)

const (
	meterName       = "metry"
	shutdownTimeout = 10 * time.Second
)

var (
	// ErrServiceNameRequired is returned when Init is called with an empty ServiceName.
	ErrServiceNameRequired = errors.New("metry: ServiceName is required")
)

// Init configures global OTel providers and returns a shutdown function.
// Uses resource.Default() merged with service attributes for host/PID/OS and service identity.
// On partial failure, already-created providers are shut down and global otel state is
// restored to the previous tracer/meter/propagator/default GenAI tracker.

//nolint:gocognit,funlen // Init orchestrates trace/metrics/propagator/session lifecycle in one place.
func Init(ctx context.Context, opts ...Option) (func(context.Context) error, error) {
	initSessionMu.Lock()
	defer initSessionMu.Unlock()

	prevSession := currentInitSession
	baseState := newBaseOTelState()
	if prevSession != nil {
		baseState = prevSession.base
	}
	prevTracer := baseState.tracer
	prevMeter := baseState.meter
	prevPropagator := baseState.propagator
	prevTracker := baseState.tracker
	if prevSession != nil {
		prevTracer = prevSession.activeTracer
		prevMeter = prevSession.activeMeter
		prevPropagator = prevSession.activePropagator
		prevTracker = prevSession.tracker
	}

	cfg := newConfig()
	for _, o := range opts {
		o(cfg)
	}
	if cfg.ServiceName == "" {
		return nil, ErrServiceNameRequired
	}

	// Custom resource with service attributes; merge with default (host, PID, OS).
	customAttrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.TelemetrySDKLanguageGo,
	}
	if cfg.ServiceVersion != "" {
		customAttrs = append(customAttrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}
	if cfg.Environment != "" {
		customAttrs = append(customAttrs, semconv.DeploymentEnvironmentName(cfg.Environment))
	}
	customRes := resource.NewWithAttributes(semconv.SchemaURL, customAttrs...)
	// Use resource.Default() until SDK provides resource.New(ctx, resource.WithDefault()) for context-aware init.
	defRes := resource.Default()
	res, err := resource.Merge(defRes, customRes)
	if err != nil {
		return nil, fmt.Errorf("metry: merge resource: %w", err)
	}

	// Trace provider.
	var tp *sdktrace.TracerProvider
	{
		exp := cfg.Exporter
		if exp == nil {
			exp = noopSpanExporter{}
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithBatcher(exp),
			sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.TraceRatio)),
		)
		otel.SetTracerProvider(tp)
	}

	// Each Init owns its active meter provider. Without a metric exporter, install a session-local no-op provider
	// so metrics are not inherited from a previous metry session.
	activeMeter := metric.MeterProvider(noopmetric.NewMeterProvider())
	var mp *sdkmetric.MeterProvider
	if cfg.MetricExporter != nil {
		mp = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(cfg.MetricExporter)),
		)
		activeMeter = mp
	}

	// W3C Trace Context and Baggage propagation.
	installedPropagator := &metryPropagator{TextMapPropagator: propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)}
	otel.SetTextMapPropagator(installedPropagator)

	var trackerMeter metric.Meter
	if cfg.MetricExporter != nil {
		trackerMeter = mp.Meter(meterName)
	}
	defaultTracker, err := genai.NewTrackerWithTracer(trackerMeter, tp.Tracer("metry/genai"), cfg.genAIOptions...)
	if err != nil {
		errs := []error{fmt.Errorf("metry: create genai tracker: %w", err)}
		applyGlobalOTelState(prevTracer, prevMeter, prevPropagator, prevTracker)
		if e := tp.Shutdown(ctx); e != nil {
			errs = append(errs, e)
		}
		if mp != nil {
			if e := mp.Shutdown(ctx); e != nil {
				errs = append(errs, e)
			}
		}
		return nil, errors.Join(errs...)
	}
	applyGlobalOTelState(tp, activeMeter, installedPropagator, defaultTracker)
	session := &initSession{
		prev:             prevSession,
		base:             baseState,
		activeTracer:     tp,
		activeMeter:      activeMeter,
		activePropagator: installedPropagator,
		tp:               tp,
		mp:               mp,
		tracker:          defaultTracker,
		closed:           false,
	}
	currentInitSession = session

	shutdownFn := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		initSessionMu.Lock()
		if session.closed {
			initSessionMu.Unlock()
			return nil
		}
		session.closed = true
		if currentInitSession == session {
			nextSession := livePreviousSession(session.prev)
			currentInitSession = nextSession
			if nextSession != nil {
				applyGlobalOTelState(
					nextSession.activeTracer,
					nextSession.activeMeter,
					nextSession.activePropagator,
					nextSession.tracker,
				)
			} else {
				applyGlobalOTelState(
					session.base.tracer,
					session.base.meter,
					session.base.propagator,
					session.base.tracker,
				)
			}
		}
		initSessionMu.Unlock()

		var errs []error
		if err := session.tp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer shutdown: %w", err))
		}
		if session.mp != nil {
			if err := session.mp.Shutdown(ctx); err != nil {
				errs = append(errs, fmt.Errorf("meter shutdown: %w", err))
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	}
	return shutdownFn, nil
}

// noopSpanExporter implements sdktrace.SpanExporter and drops all spans.
type noopSpanExporter struct{}

func (noopSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (noopSpanExporter) Shutdown(context.Context) error                             { return nil }
