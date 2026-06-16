package metry

import (
	"errors"
	"testing"

	"github.com/skosovsky/metry/internal/genaiwire"
	"github.com/skosovsky/metry/internal/metrytestwire"
)

func TestWireInit_GenAIWireHooksNonNil(t *testing.T) {
	if genaiwire.MeterTracer == nil {
		t.Fatal("genaiwire.MeterTracer is nil after init")
	}
	if genaiwire.NewHintSampler == nil {
		t.Fatal("genaiwire.NewHintSampler is nil after init")
	}
}

func TestWireInit_MetryTestWireHooksNonNil(t *testing.T) {
	if metrytestwire.SpanExporter == nil {
		t.Fatal("metrytestwire.SpanExporter is nil after init")
	}
	if metrytestwire.MetricExporter == nil {
		t.Fatal("metrytestwire.MetricExporter is nil after init")
	}
	if metrytestwire.NewProviderFromDeps == nil {
		t.Fatal("metrytestwire.NewProviderFromDeps is nil after init")
	}
}

func TestWireInit_NewHintSampler_RejectsWrongType(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for non-TraceSampler base")
		}
	}()
	_ = genaiwire.NewHintSampler("not-a-sampler")
}

func TestWireInit_MeterTracer_RejectsWrongType(t *testing.T) {
	_, _, err := genaiwire.MeterTracer("not-a-provider")
	if !errors.Is(err, ErrInvalidProviderType) {
		t.Fatalf("expected ErrInvalidProviderType, got %v", err)
	}
}

func TestWireInit_MeterTracer_NilProvider_ReturnsErrNilProvider(t *testing.T) {
	_, _, err := genaiwire.MeterTracer((*Provider)(nil))
	if !errors.Is(err, ErrNilProvider) {
		t.Fatalf("expected ErrNilProvider, got %v", err)
	}
}

func TestWireInit_MeterTracer_PartialProvider_ReturnsErrNilMeterProvider(t *testing.T) {
	_, _, err := genaiwire.MeterTracer(&Provider{})
	if !errors.Is(err, ErrNilMeterProvider) {
		t.Fatalf("expected ErrNilMeterProvider, got %v", err)
	}
}

func TestWireInit_MeterTracer_PartialTracer_ReturnsErrNilTracerProvider(t *testing.T) {
	provider, _ := newTestProvider(t)
	provider.otelTracer = nil
	_, _, err := genaiwire.MeterTracer(provider)
	if !errors.Is(err, ErrNilTracerProvider) {
		t.Fatalf("expected ErrNilTracerProvider, got %v", err)
	}
}
