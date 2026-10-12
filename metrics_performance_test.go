package metry

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

var performanceAttributesSink []attribute.KeyValue

func performanceLabels(n int) Labels {
	labels := make(Labels, n)
	for i := range n {
		labels[fmt.Sprintf("key%03d", i)] = "value"
	}
	return labels
}

func TestMetricAllocationBudgets(t *testing.T) {
	for _, n := range []int{0, 8, 64, 256} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			// Arrange.
			labels := performanceLabels(n)
			ctx := context.Background()
			// Act.
			conversion := testing.AllocsPerRun(100, func() { performanceAttributesSink = labelsToAttributes(labels) })
			disabled := testing.AllocsPerRun(100, func() {
				CounterMetric{}.Add(ctx, 1, labels)
				HistogramMetric{}.Record(ctx, 1, labels)
				GaugeMetric{}.Record(ctx, 1, labels)
			})
			// Assert.
			budget := float64(1)
			if n == 0 {
				budget = 0
			}
			if conversion > budget || disabled != 0 {
				t.Fatalf("conversion=%g budget=%g disabled=%g", conversion, budget, disabled)
			}
		})
	}
}

func TestMetricRecordingOwnsAttributes(t *testing.T) {
	// Arrange.
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(ctx) })
	reg := NewMetricsRegistry(&Provider{otelMeter: mp})
	counter, err := reg.NewCounter("ownership_counter")
	if err != nil {
		t.Fatal(err)
	}
	hist, err := reg.NewHistogram("ownership_histogram", []float64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	gauge, err := reg.NewGauge("ownership_gauge")
	if err != nil {
		t.Fatal(err)
	}
	labels := Labels{"status": "original", "": "invalid", "empty": ""}
	// Act.
	attrs := labelsToAttributes(labels)
	counter.Add(ctx, 3, labels)
	hist.Record(ctx, 1.5, labels)
	gauge.Record(ctx, 42.5, labels)
	labels["status"] = "changed"
	labels["new"] = "later"
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	// Assert.
	if len(attrs) != 1 || attrs[0] != attribute.String("status", "original") {
		t.Fatalf("attributes=%v", attrs)
	}
	seen := 0
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			var set attribute.Set
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				if len(data.DataPoints) != 1 || data.DataPoints[0].Value != 3 {
					t.Fatalf("counter=%v", data)
				}
				set = data.DataPoints[0].Attributes
				if data.DataPoints[0].Time.IsZero() || data.DataPoints[0].StartTime.IsZero() {
					t.Fatal("missing counter timestamps")
				}
			case metricdata.Histogram[float64]:
				if len(data.DataPoints) != 1 || data.DataPoints[0].Sum != 1.5 || data.DataPoints[0].Count != 1 || len(data.DataPoints[0].Bounds) != 2 {
					t.Fatalf("histogram=%v", data)
				}
				set = data.DataPoints[0].Attributes
				if data.DataPoints[0].Time.IsZero() || data.DataPoints[0].StartTime.IsZero() {
					t.Fatal("missing histogram timestamps")
				}
			case metricdata.Gauge[float64]:
				if len(data.DataPoints) != 1 || data.DataPoints[0].Value != 42.5 {
					t.Fatalf("gauge=%v", data)
				}
				set = data.DataPoints[0].Attributes
				if data.DataPoints[0].Time.IsZero() {
					t.Fatal("missing gauge timestamp")
				}
			default:
				t.Fatalf("unexpected metric %T", m.Data)
			}
			value, ok := set.Value("status")
			if !ok || value.AsString() != "original" || set.Len() != 1 || m.Unit != "" {
				t.Fatalf("metric=%+v attributes=%v", m, set)
			}
			seen++
		}
	}
	if seen != 3 {
		t.Fatalf("metrics=%d want=3", seen)
	}
}

func BenchmarkPerformanceLabels(b *testing.B) {
	for _, n := range []int{0, 8, 64, 256} {
		for _, mode := range []string{"direct", "copy_control"} {
			b.Run(fmt.Sprintf("%s/%d", mode, n), func(b *testing.B) {
				labels := performanceLabels(n)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if mode == "direct" {
						performanceAttributesSink = labelsToAttributes(labels)
					} else {
						performanceAttributesSink = labelsToAttributes(LabelsOf(labels))
					}
				}
			})
		}
	}
}

func BenchmarkPerformanceRecording(b *testing.B) {
	for _, backend := range []string{"noop", "memory", "disabled"} {
		for _, kind := range []string{"counter", "histogram", "gauge"} {
			for _, n := range []int{0, 8, 64, 256} {
				b.Run(fmt.Sprintf("%s/%s/%d", backend, kind, n), func(b *testing.B) {
					ctx := context.Background()
					labels := performanceLabels(n)
					var meter metric.Meter
					if backend == "memory" {
						mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
						b.Cleanup(func() { _ = mp.Shutdown(ctx) })
						meter = mp.Meter("performance")
					} else {
						meter = noop.NewMeterProvider().Meter("performance")
					}
					cnt, err := meter.Int64Counter("counter")
					if err != nil {
						b.Fatal(err)
					}
					hist, err := meter.Float64Histogram("histogram")
					if err != nil {
						b.Fatal(err)
					}
					gauge, err := meter.Float64Gauge("gauge")
					if err != nil {
						b.Fatal(err)
					}
					c := CounterMetric{cnt: cnt}
					h := HistogramMetric{hist: hist}
					g := GaugeMetric{gauge: gauge}
					if backend == "disabled" {
						c = CounterMetric{}
						h = HistogramMetric{}
						g = GaugeMetric{}
					}
					record := func() {
						switch kind {
						case "counter":
							c.Add(ctx, 1, labels)
						case "histogram":
							h.Record(ctx, 1, labels)
						case "gauge":
							g.Record(ctx, 1, labels)
						}
					}
					record() // Warm the one bounded series outside the timer on both revisions.
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						record()
					}
				})
			}
		}
	}
}
