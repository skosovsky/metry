package executor

import (
	"context"

	"github.com/skosovsky/metry"
)

// Option configures Wrap.
type Option = metry.ExecutorWrapOption

// WithLogger sets the [slog.Logger] used for optional start and error/panic logs.
//
//nolint:gochecknoglobals // thin alias to metry root API
var WithLogger = metry.WithExecutorLogger

// WithLogStart enables an Info log at the beginning of each invocation (requires WithLogger).
//
//nolint:gochecknoglobals // thin alias to metry root API
var WithLogStart = metry.WithExecutorLogStart

// WithLogError enables Error logs when next returns an error or panics (requires WithLogger). Default is true.
//
//nolint:gochecknoglobals // thin alias to metry root API
var WithLogError = metry.WithExecutorLogError

// Wrap returns a function that runs next with a span, standard executor metrics, and optional slog logs.
func Wrap[Req, Res any](
	provider *metry.Provider,
	operationName string,
	next func(context.Context, Req) (Res, error),
	opts ...Option,
) func(context.Context, Req) (Res, error) {
	return metry.ExecutorWrap(provider, operationName, next, opts...)
}
