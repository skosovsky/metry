package metry

import (
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/internal/defaultslot"
)

type baseOTelState struct {
	tracer     trace.TracerProvider
	meter      metric.MeterProvider
	propagator propagation.TextMapPropagator
	tracker    *genai.Tracker
}

type initSession struct {
	prev             *initSession
	base             *baseOTelState
	activeTracer     trace.TracerProvider
	activeMeter      metric.MeterProvider
	activePropagator propagation.TextMapPropagator
	tp               *sdktrace.TracerProvider
	mp               *sdkmetric.MeterProvider
	tracker          *genai.Tracker
	closed           bool
}

type metryPropagator struct {
	propagation.TextMapPropagator
}

var (
	initSessionMu      sync.Mutex
	currentInitSession *initSession
)

func newBaseOTelState() *baseOTelState {
	var tracker *genai.Tracker
	if current := defaultslot.Load(); current != nil {
		tracker, _ = current.(*genai.Tracker)
	}
	return &baseOTelState{
		tracer:     otel.GetTracerProvider(),
		meter:      otel.GetMeterProvider(),
		propagator: otel.GetTextMapPropagator(),
		tracker:    tracker,
	}
}

func livePreviousSession(session *initSession) *initSession {
	for session != nil && session.closed {
		session = session.prev
	}
	return session
}

func applyGlobalOTelState(
	tracer trace.TracerProvider,
	meter metric.MeterProvider,
	propagator propagation.TextMapPropagator,
	tracker *genai.Tracker,
) {
	otel.SetTracerProvider(tracer)
	otel.SetMeterProvider(meter)
	otel.SetTextMapPropagator(propagator)
	defaultslot.Swap(tracker)
}
