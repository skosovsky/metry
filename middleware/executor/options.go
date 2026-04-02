package executor

import "log/slog"

type config struct {
	logger   *slog.Logger
	logStart bool
	logError bool
}

func defaultConfig() config {
	return config{
		logger:   nil,
		logStart: false,
		logError: true,
	}
}

// Option configures Wrap.
type Option func(*config)

// WithLogger sets the slog logger used for optional start and error/panic logs.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		c.logger = l
	}
}

// WithLogStart enables an Info log at the beginning of each invocation (requires WithLogger).
func WithLogStart(v bool) Option {
	return func(c *config) {
		c.logStart = v
	}
}

// WithLogError enables Error logs when next returns an error or panics (requires WithLogger). Default is true.
func WithLogError(v bool) Option {
	return func(c *config) {
		c.logError = v
	}
}
