package genai

const defaultMaxContextLength = 65536

// Option configures tracker runtime behavior.
type Option func(*runtimeConfig)

type runtimeConfig struct {
	recordPayloads   bool
	maxContextLength int
}

func defaultRuntimeConfig() runtimeConfig {
	return runtimeConfig{
		recordPayloads:   false,
		maxContextLength: defaultMaxContextLength,
	}
}

func buildRuntimeConfig(opts ...Option) runtimeConfig {
	cfg := defaultRuntimeConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.maxContextLength <= 0 {
		cfg.maxContextLength = defaultMaxContextLength
	}
	return cfg
}

func (c runtimeConfig) RecordPayloads() bool {
	return c.recordPayloads
}

func (c runtimeConfig) MaxContextLength() int {
	if c.maxContextLength <= 0 {
		return defaultMaxContextLength
	}
	return c.maxContextLength
}

// WithRecordPayloads enables payload recording on spans for this tracker.
func WithRecordPayloads(enabled bool) Option {
	return func(c *runtimeConfig) {
		c.recordPayloads = enabled
	}
}

// WithMaxContextLength sets the payload truncation limit for this tracker.
func WithMaxContextLength(bytes int) Option {
	return func(c *runtimeConfig) {
		c.maxContextLength = bytes
	}
}
