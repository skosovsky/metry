package executor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// instrumentsCache keys by (MeterProvider identity, meter scope). We cannot key by metric.Meter:
// noop and other implementations may use non-pointer concrete types, which break [reflect.Pointer].
type instrumentsCacheKey struct {
	meterProviderPtr uintptr
	scope            string
}

//nolint:gochecknoglobals // process-wide instrument cache; keys isolate distinct MeterProvider instances.
var instrumentsCache sync.Map // instrumentsCacheKey -> *instruments

var errInstrumentsCacheCorrupt = errors.New("metry/executor: instruments cache corrupted")

const (
	meterName = "metry.executor"

	durationMetricName = "executor.operation.duration"
	callsMetricName    = "executor.operation.calls"
)

type instruments struct {
	duration metric.Float64Histogram
	calls    metric.Int64Counter
}

func newInstruments(m metric.Meter) (*instruments, error) {
	hist, err := m.Float64Histogram(
		durationMetricName,
		metric.WithDescription("Duration of wrapped executor operations in seconds."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("metry/executor: histogram %s: %w", durationMetricName, err)
	}
	cnt, err := m.Int64Counter(
		callsMetricName,
		metric.WithDescription("Number of wrapped executor operation invocations."),
	)
	if err != nil {
		return nil, fmt.Errorf("metry/executor: counter %s: %w", callsMetricName, err)
	}
	return &instruments{duration: hist, calls: cnt}, nil
}

func meterProviderIdentity(mp metric.MeterProvider) (uintptr, bool) {
	if mp == nil {
		return 0, false
	}
	return reflectValueIdentity(reflect.ValueOf(mp))
}

func reflectValueIdentity(v reflect.Value) (uintptr, bool) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return 0, false
		}
		return v.Pointer(), true
	case reflect.Interface:
		if v.IsNil() {
			return 0, false
		}
		return reflectValueIdentity(v.Elem())
	default:
		return 0, false
	}
}

// getOrCreateInstruments returns cached instruments for mp.Meter(scope), creating on first use per provider+scope.
func getOrCreateInstruments(mp metric.MeterProvider, scope string) (*instruments, error) {
	id, ok := meterProviderIdentity(mp)
	if !ok {
		return newInstruments(mp.Meter(scope))
	}
	key := instrumentsCacheKey{meterProviderPtr: id, scope: scope}
	if v, ok := instrumentsCache.Load(key); ok {
		out, okCast := v.(*instruments)
		if !okCast {
			return nil, errInstrumentsCacheCorrupt
		}
		return out, nil
	}
	inst, err := newInstruments(mp.Meter(scope))
	if err != nil {
		return nil, err
	}
	if v, loaded := instrumentsCache.LoadOrStore(key, inst); loaded {
		out, okCast := v.(*instruments)
		if !okCast {
			return nil, errInstrumentsCacheCorrupt
		}
		return out, nil
	}
	return inst, nil
}

func (i *instruments) record(ctx context.Context, operation, status string, durationSec float64) {
	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("status", status),
	}
	i.duration.Record(ctx, durationSec, metric.WithAttributes(attrs...))
	i.calls.Add(ctx, 1, metric.WithAttributes(attrs...))
}
