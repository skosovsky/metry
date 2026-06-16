package metry

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ErrDuplicateMetric is returned when registering the same metric name twice.
var ErrDuplicateMetric = errors.New("metry: duplicate metric name")

// MetricsRegistry registers domain metrics at init time.
type MetricsRegistry struct {
	meter      metric.Meter
	mu         sync.Mutex
	names      map[string]struct{}
	histograms map[string]metric.Float64Histogram
	counters   map[string]metric.Int64Counter
	gauges     map[string]metric.Float64Gauge
}

var ErrMetricsRegistryNotConfigured = errors.New("metry: metrics registry is not configured")

func newEmptyMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{
		meter:      nil,
		names:      make(map[string]struct{}),
		histograms: make(map[string]metric.Float64Histogram),
		counters:   make(map[string]metric.Int64Counter),
		gauges:     make(map[string]metric.Float64Gauge),
		mu:         sync.Mutex{},
	}
}

// NewMetricsRegistry creates a registry backed by the provider meter.
func NewMetricsRegistry(p *Provider) *MetricsRegistry {
	if p == nil || p.otelMeter == nil {
		return newEmptyMetricsRegistry()
	}
	return &MetricsRegistry{
		meter:      p.otelMeter.Meter("metry/domain"),
		names:      make(map[string]struct{}),
		histograms: make(map[string]metric.Float64Histogram),
		counters:   make(map[string]metric.Int64Counter),
		gauges:     make(map[string]metric.Float64Gauge),
		mu:         sync.Mutex{},
	}
}

func (r *MetricsRegistry) registerName(name string) error {
	if _, exists := r.names[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateMetric, name)
	}
	r.names[name] = struct{}{}
	return nil
}

// HistogramMetric records float64 observations with typed labels.
type HistogramMetric struct {
	hist metric.Float64Histogram
}

// OK reports whether the histogram instrument was registered successfully.
func (h HistogramMetric) OK() bool {
	return h.hist != nil
}

// Record observes value with labels applied as attributes.
func (h HistogramMetric) Record(ctx context.Context, value float64, labels Labels) {
	if h.hist == nil {
		return
	}
	attrs := labelsToAttributes(copyLabels(labels))
	h.hist.Record(ctx, value, metric.WithAttributes(attrs...))
}

// CounterMetric records int64 counter increments with typed labels.
type CounterMetric struct {
	cnt metric.Int64Counter
}

// OK reports whether the counter instrument was registered successfully.
func (c CounterMetric) OK() bool {
	return c.cnt != nil
}

// Add increments the counter by value with labels applied as attributes.
func (c CounterMetric) Add(ctx context.Context, value int64, labels Labels) {
	if c.cnt == nil {
		return
	}
	attrs := labelsToAttributes(copyLabels(labels))
	c.cnt.Add(ctx, value, metric.WithAttributes(attrs...))
}

// GaugeMetric records float64 gauge observations with typed labels.
type GaugeMetric struct {
	gauge metric.Float64Gauge
}

// OK reports whether the gauge instrument was registered successfully.
func (g GaugeMetric) OK() bool {
	return g.gauge != nil
}

// Record observes value with labels applied as attributes.
func (g GaugeMetric) Record(ctx context.Context, value float64, labels Labels) {
	if g.gauge == nil {
		return
	}
	attrs := labelsToAttributes(copyLabels(labels))
	g.gauge.Record(ctx, value, metric.WithAttributes(attrs...))
}

// NewHistogram registers a histogram metric. Returns ErrDuplicateMetric if name exists.
func (r *MetricsRegistry) NewHistogram(name string, buckets []float64) (HistogramMetric, error) {
	if r == nil || r.meter == nil {
		return HistogramMetric{}, ErrMetricsRegistryNotConfigured
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.registerName(name); err != nil {
		return HistogramMetric{}, err
	}
	opts := []metric.Float64HistogramOption{}
	if len(buckets) > 0 {
		opts = append(opts, metric.WithExplicitBucketBoundaries(buckets...))
	}
	hist, err := r.meter.Float64Histogram(name, opts...)
	if err != nil {
		delete(r.names, name)
		return HistogramMetric{}, fmt.Errorf("metry: create histogram %q: %w", name, err)
	}
	r.histograms[name] = hist
	return HistogramMetric{hist: hist}, nil
}

// NewCounter registers a counter metric. Returns ErrDuplicateMetric if name exists.
func (r *MetricsRegistry) NewCounter(name string) (CounterMetric, error) {
	if r == nil || r.meter == nil {
		return CounterMetric{}, ErrMetricsRegistryNotConfigured
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.registerName(name); err != nil {
		return CounterMetric{}, err
	}
	cnt, err := r.meter.Int64Counter(name)
	if err != nil {
		delete(r.names, name)
		return CounterMetric{}, fmt.Errorf("metry: create counter %q: %w", name, err)
	}
	r.counters[name] = cnt
	return CounterMetric{cnt: cnt}, nil
}

// NewGauge registers a gauge metric. Returns ErrDuplicateMetric if name exists.
func (r *MetricsRegistry) NewGauge(name string) (GaugeMetric, error) {
	if r == nil || r.meter == nil {
		return GaugeMetric{}, ErrMetricsRegistryNotConfigured
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.registerName(name); err != nil {
		return GaugeMetric{}, err
	}
	gauge, err := r.meter.Float64Gauge(name)
	if err != nil {
		delete(r.names, name)
		return GaugeMetric{}, fmt.Errorf("metry: create gauge %q: %w", name, err)
	}
	r.gauges[name] = gauge
	return GaugeMetric{gauge: gauge}, nil
}

func labelsToAttributes(labels Labels) []attribute.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(labels))
	for k, v := range labels {
		if k == "" || v == "" {
			continue
		}
		attrs = append(attrs, attribute.String(k, v))
	}
	return attrs
}
