package metry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"
)

const executorTracerName = "github.com/skosovsky/metry/middleware/executor"

const (
	executorMeterName          = executorTracerName
	executorDurationMetricName = "executor_operation_duration"
	executorCallsMetricName    = "executor_operation_calls"
)

// ExecutorWrapOption configures ExecutorWrap.
type ExecutorWrapOption func(*executorWrapConfig)

type executorWrapConfig struct {
	logger   *slog.Logger
	logStart bool
	logError bool
}

func defaultExecutorWrapConfig() executorWrapConfig {
	return executorWrapConfig{
		logger:   nil,
		logStart: false,
		logError: true,
	}
}

// WithExecutorLogger sets the slog logger used for optional start and error/panic logs.
func WithExecutorLogger(l *slog.Logger) ExecutorWrapOption {
	return func(c *executorWrapConfig) {
		c.logger = l
	}
}

// WithExecutorLogStart enables an Info log at the beginning of each invocation (requires WithExecutorLogger).
func WithExecutorLogStart(v bool) ExecutorWrapOption {
	return func(c *executorWrapConfig) {
		c.logStart = v
	}
}

// WithExecutorLogError enables Error logs when next returns an error or panics (requires WithExecutorLogger). Default is true.
func WithExecutorLogError(v bool) ExecutorWrapOption {
	return func(c *executorWrapConfig) {
		c.logError = v
	}
}

// ExecutorWrap returns a function that runs next with a span, standard executor metrics, and optional slog logs.
func ExecutorWrap[Req, Res any](
	provider *Provider,
	operationName string,
	next func(context.Context, Req) (Res, error),
	opts ...ExecutorWrapOption,
) func(context.Context, Req) (Res, error) {
	if provider == nil {
		panic("metry: provider is required")
	}
	if next == nil {
		panic("metry: next is required")
	}

	cfg := defaultExecutorWrapConfig()
	for _, o := range opts {
		o(&cfg)
	}

	instr, err := getOrCreateExecutorInstruments(provider.meterProvider(), executorMeterName)
	if err != nil {
		panic(err)
	}
	tracer := provider.tracerProvider().Tracer(executorTracerName)

	return func(ctx context.Context, req Req) (Res, error) {
		ctx, span := tracer.Start(ctx, operationName)
		defer span.End()

		if cfg.logStart && cfg.logger != nil {
			args := []any{slog.String("operation", operationName)}
			args = append(args, executorTraceSlogFields(span)...)
			cfg.logger.InfoContext(ctx, "executor start", args...)
		}

		start := time.Now()

		var res Res
		var runErr error
		defer func() {
			dur := time.Since(start).Seconds()
			if r := recover(); r != nil {
				recordExecutorPanicOutcome(ctx, span, instr, operationName, dur, r, cfg)
				panic(r)
			}
			recordExecutorReturnOutcome(ctx, span, instr, operationName, dur, runErr, cfg)
		}()

		res, runErr = next(ctx, req)
		return res, runErr
	}
}

func recordExecutorPanicOutcome(
	ctx context.Context,
	span trace.Span,
	instr *executorInstruments,
	operationName string,
	dur float64,
	r any,
	cfg executorWrapConfig,
) {
	panicErr := fmt.Errorf("panic: %v", r)
	traceutil.SpanError(span, panicErr)
	instr.record(ctx, operationName, "panic", dur)
	if cfg.logError && cfg.logger != nil {
		args := []any{
			slog.String("operation", operationName),
			slog.Any("panic", r),
		}
		args = append(args, executorTraceSlogFields(span)...)
		cfg.logger.ErrorContext(ctx, "executor panic", args...)
	}
}

func recordExecutorReturnOutcome(
	ctx context.Context,
	span trace.Span,
	instr *executorInstruments,
	operationName string,
	dur float64,
	err error,
	cfg executorWrapConfig,
) {
	status := "success"
	if err != nil {
		status = "error"
		traceutil.SpanError(span, err)
		if cfg.logError && cfg.logger != nil {
			args := []any{
				slog.String("operation", operationName),
				slog.Any("err", err),
			}
			args = append(args, executorTraceSlogFields(span)...)
			cfg.logger.ErrorContext(ctx, "executor error", args...)
		}
	} else {
		traceutil.SpanOK(span)
	}
	instr.record(ctx, operationName, status, dur)
}

func executorTraceSlogFields(span trace.Span) []any {
	sc := span.SpanContext()
	if !sc.IsValid() || !sc.HasTraceID() {
		return nil
	}
	return []any{slog.String("trace_id", sc.TraceID().String())}
}

const (
	// ExecutorDurationMetricName is the histogram instrument name used by ExecutorWrap.
	ExecutorDurationMetricName = executorDurationMetricName
	// ExecutorCallsMetricName is the counter instrument name used by ExecutorWrap.
	ExecutorCallsMetricName = executorCallsMetricName
)

type executorInstrumentsCacheKey struct {
	meterProviderPtr uintptr
	scope            string
}

//nolint:gochecknoglobals // process-wide instrument cache; keys isolate distinct MeterProvider instances.
var executorInstrumentsCache sync.Map // executorInstrumentsCacheKey -> *executorInstruments

var errExecutorInstrumentsCacheCorrupt = errors.New("metry: executor instruments cache corrupted")

type executorInstruments struct {
	duration metric.Float64Histogram
	calls    metric.Int64Counter
}

func newExecutorInstruments(m metric.Meter) (*executorInstruments, error) {
	hist, err := m.Float64Histogram(
		executorDurationMetricName,
		metric.WithDescription("Duration of wrapped executor operations in seconds."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("metry: histogram %s: %w", executorDurationMetricName, err)
	}
	cnt, err := m.Int64Counter(
		executorCallsMetricName,
		metric.WithDescription("Number of wrapped executor operation invocations."),
	)
	if err != nil {
		return nil, fmt.Errorf("metry: counter %s: %w", executorCallsMetricName, err)
	}
	return &executorInstruments{duration: hist, calls: cnt}, nil
}

func executorMeterProviderIdentity(mp metric.MeterProvider) (uintptr, bool) {
	if mp == nil {
		return 0, false
	}
	return executorReflectValueIdentity(reflect.ValueOf(mp))
}

func executorReflectValueIdentity(v reflect.Value) (uintptr, bool) {
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
		return executorReflectValueIdentity(v.Elem())
	default:
		return 0, false
	}
}

func getOrCreateExecutorInstruments(mp metric.MeterProvider, scope string) (*executorInstruments, error) {
	id, ok := executorMeterProviderIdentity(mp)
	if !ok {
		return newExecutorInstruments(mp.Meter(scope))
	}
	key := executorInstrumentsCacheKey{meterProviderPtr: id, scope: scope}
	if v, ok := executorInstrumentsCache.Load(key); ok {
		out, okCast := v.(*executorInstruments)
		if !okCast {
			return nil, errExecutorInstrumentsCacheCorrupt
		}
		return out, nil
	}
	inst, err := newExecutorInstruments(mp.Meter(scope))
	if err != nil {
		return nil, err
	}
	if v, loaded := executorInstrumentsCache.LoadOrStore(key, inst); loaded {
		out, okCast := v.(*executorInstruments)
		if !okCast {
			return nil, errExecutorInstrumentsCacheCorrupt
		}
		return out, nil
	}
	return inst, nil
}

func (i *executorInstruments) record(ctx context.Context, operation, status string, durationSec float64) {
	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("status", status),
	}
	i.duration.Record(ctx, durationSec, metric.WithAttributes(attrs...))
	i.calls.Add(ctx, 1, metric.WithAttributes(attrs...))
}
