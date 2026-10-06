package metry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type shutdownProcessor struct {
	entered chan context.Context
	release chan struct{}
	err     error
	calls   atomic.Int32
}

func (*shutdownProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (*shutdownProcessor) OnEnd(sdktrace.ReadOnlySpan)                     {}
func (*shutdownProcessor) ForceFlush(context.Context) error                { return nil }
func (s *shutdownProcessor) Shutdown(ctx context.Context) error {
	s.calls.Add(1)
	s.entered <- ctx
	<-s.release
	return s.err
}

type shutdownReader struct {
	sdkmetric.Reader

	err   error
	calls atomic.Int32
}

func (r *shutdownReader) Shutdown(ctx context.Context) error {
	r.calls.Add(1)
	return errors.Join(r.Reader.Shutdown(ctx), r.err)
}

func TestOwnedShutdownConcurrentCallersWaitForSameFirstResult(t *testing.T) {
	// Arrange: real owned SDKs with controlled failure/wait witnesses.
	traceFailure, metricFailure := errors.New("trace failed"), errors.New("meter failed")
	processor := &shutdownProcessor{
		entered: make(chan context.Context, 1), release: make(chan struct{}), err: traceFailure,
	}
	reader := &shutdownReader{Reader: sdkmetric.NewManualReader(), err: metricFailure}
	provider := &Provider{
		tp: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor)),
		mp: sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
	}
	first := make(chan error, 1)
	go func() { first <- provider.Shutdown(context.Background()) }()
	var shutdownCtx context.Context
	select {
	case shutdownCtx = <-processor.entered:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not reach the owned pipeline")
	}
	defer func() {
		select {
		case <-processor.release:
		default:
			close(processor.release)
		}
	}()
	deadline, ok := shutdownCtx.Deadline()
	require.True(t, ok)
	require.Positive(t, time.Until(deadline))
	require.LessOrEqual(t, time.Until(deadline), shutdownTimeout)
	// Act: a later cancelled caller still waits for the first terminal operation.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	started, second := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		second <- provider.Shutdown(cancelled)
	}()
	<-started
	select {
	case <-second:
		t.Fatal("concurrent shutdown returned before the first completed")
	case <-time.After(25 * time.Millisecond):
	}
	close(processor.release)
	result := <-first
	other := <-second
	var joined sync.WaitGroup
	results := make(chan error, 8)
	for range cap(results) {
		joined.Go(func() { results <- provider.Shutdown(context.Background()) })
	}
	joined.Wait()
	close(results)
	// Assert: both owned pipelines are attempted once; every result is identical.
	require.ErrorIs(t, result, traceFailure)
	require.ErrorIs(t, result, metricFailure)
	require.Same(t, result, other)
	for subsequent := range results {
		require.Same(t, result, subsequent)
	}
	require.EqualValues(t, 1, processor.calls.Load())
	require.EqualValues(t, 1, reader.calls.Load())
}

func TestOwnedShutdownFirstCancelledContextRemainsTerminal(t *testing.T) {
	// Arrange.
	processor := &shutdownProcessor{entered: make(chan context.Context, 1), release: make(chan struct{})}
	close(processor.release)
	provider := &Provider{tp: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Act.
	first := provider.Shutdown(ctx)
	second := provider.Shutdown(context.Background())
	// Assert: SDK shutdown is terminal even before it can reach processors.
	require.ErrorIs(t, first, context.Canceled)
	require.Same(t, first, second)
	require.Zero(t, processor.calls.Load())
}

func TestOwnedShutdownRespectsEarlierCallerDeadline(t *testing.T) {
	// Arrange.
	processor := &shutdownProcessor{entered: make(chan context.Context, 1), release: make(chan struct{})}
	close(processor.release)
	provider := &Provider{tp: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	want, ok := ctx.Deadline()
	require.True(t, ok)
	// Act.
	err := provider.Shutdown(ctx)
	observed := <-processor.entered
	// Assert.
	require.NoError(t, err)
	got, ok := observed.Deadline()
	require.True(t, ok)
	require.Equal(t, want, got)
}
