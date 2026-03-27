package genai

const defaultMaxContextLength = 65536

// defaultMaxEventLength is the default cap for text stored on span events (e.g. evaluation reasoning).
// Many backends enforce stricter quotas on event attribute payloads than on span attributes (~4–8KB).
const defaultMaxEventLength = 4096

// Option configures tracker runtime behavior.
type Option func(*runtimeConfig)

type runtimeConfig struct {
	recordPayloads   bool
	maxContextLength int
	maxEventLength   int
}

func defaultRuntimeConfig() runtimeConfig {
	return runtimeConfig{
		recordPayloads:   false,
		maxContextLength: defaultMaxContextLength,
		maxEventLength:   defaultMaxEventLength,
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
	if cfg.maxEventLength <= 0 {
		cfg.maxEventLength = defaultMaxEventLength
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

// MaxEventLength returns the max byte length for text attached to span events (e.g. evaluation reasoning).
func (c runtimeConfig) MaxEventLength() int {
	if c.maxEventLength <= 0 {
		return defaultMaxEventLength
	}
	return c.maxEventLength
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

// WithMaxEventLength sets the max byte length for text stored on span events (e.g. LLM-judge reasoning on evaluation events).
// Backends and collectors often apply tighter limits to event attributes than to span attributes (commonly on the order of 4–8KB);
// the default is 4096 bytes. Values <= 0 are replaced with that default during config build.
func WithMaxEventLength(bytes int) Option {
	return func(c *runtimeConfig) {
		c.maxEventLength = bytes
	}
}
