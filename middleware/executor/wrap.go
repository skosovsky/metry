package executor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/traceutil"
)

const tracerName = "github.com/skosovsky/metry/middleware/executor"

// Wrap returns a function that runs next with a span, standard executor metrics, and optional slog logs.
// Metrics use meter name "metry.executor" and instrument names executor.operation.duration / executor.operation.calls
// with attributes operation (the operationName) and status (success, error, or panic).
func Wrap[Req, Res any](
	provider *metry.Provider,
	operationName string,
	next func(context.Context, Req) (Res, error),
	opts ...Option,
) func(context.Context, Req) (Res, error) {
	if provider == nil {
		panic("metry/executor: provider is required")
	}
	if provider.TracerProvider == nil {
		panic("metry/executor: tracer provider is required")
	}
	if provider.MeterProvider == nil {
		panic("metry/executor: meter provider is required")
	}
	if next == nil {
		panic("metry/executor: next is required")
	}

	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	instr, err := getOrCreateInstruments(provider.MeterProvider, meterName)
	if err != nil {
		panic(err)
	}
	tracer := provider.TracerProvider.Tracer(tracerName)

	return func(ctx context.Context, req Req) (Res, error) {
		ctx, span := tracer.Start(ctx, operationName)
		defer span.End()

		if cfg.logStart && cfg.logger != nil {
			args := []any{slog.String("operation", operationName)}
			args = append(args, traceSlogFields(span)...)
			cfg.logger.InfoContext(ctx, "executor start", args...)
		}

		start := time.Now()

		var res Res
		var err error
		defer func() {
			dur := time.Since(start).Seconds()
			if r := recover(); r != nil {
				recordPanicOutcome(ctx, span, instr, operationName, dur, r, cfg)
				panic(r)
			}
			recordReturnOutcome(ctx, span, instr, operationName, dur, err, cfg)
		}()

		res, err = next(ctx, req)
		return res, err
	}
}

func recordPanicOutcome(
	ctx context.Context,
	span trace.Span,
	instr *instruments,
	operationName string,
	dur float64,
	r any,
	cfg config,
) {
	panicErr := fmt.Errorf("panic: %v", r)
	span.RecordError(panicErr)
	span.SetStatus(codes.Error, panicErr.Error())
	instr.record(ctx, operationName, "panic", dur)
	if cfg.logError && cfg.logger != nil {
		args := []any{
			slog.String("operation", operationName),
			slog.Any("panic", r),
		}
		args = append(args, traceSlogFields(span)...)
		cfg.logger.ErrorContext(ctx, "executor panic", args...)
	}
}

func recordReturnOutcome(
	ctx context.Context,
	span trace.Span,
	instr *instruments,
	operationName string,
	dur float64,
	err error,
	cfg config,
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
			args = append(args, traceSlogFields(span)...)
			cfg.logger.ErrorContext(ctx, "executor error", args...)
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}
	instr.record(ctx, operationName, status, dur)
}

func traceSlogFields(span trace.Span) []any {
	sc := span.SpanContext()
	if !sc.IsValid() || !sc.HasTraceID() {
		return nil
	}
	return []any{slog.String("trace_id", sc.TraceID().String())}
}
