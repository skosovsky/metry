// Package genaiconfig stores the global GenAI runtime config shared between metry.Init and the genai helpers.
package genaiconfig

import (
	"math"
	"sync/atomic"
)

// DefaultMaxContextLength is the privacy-safe default truncation limit for payload-like text attributes.
const DefaultMaxContextLength = 16384

// RuntimeConfig holds immutable GenAI runtime behavior configured by metry.Init.
type RuntimeConfig struct {
	maxContextLength int
	recordPayloads   bool
}

var defaultConfig = &RuntimeConfig{
	maxContextLength: DefaultMaxContextLength,
	recordPayloads:   false,
}

var globalConfig atomic.Pointer[RuntimeConfig]

func init() {
	globalConfig.Store(defaultConfig)
}

// New returns a normalized immutable config instance suitable for Store/CompareAndSwap ownership tokens.
func New(maxContextLength int, recordPayloads bool) *RuntimeConfig {
	if maxContextLength <= 0 {
		maxContextLength = DefaultMaxContextLength
	}
	if maxContextLength > math.MaxInt32 {
		maxContextLength = math.MaxInt32
	}
	return &RuntimeConfig{
		maxContextLength: maxContextLength,
		recordPayloads:   recordPayloads,
	}
}

// Default returns the shared default runtime config.
func Default() *RuntimeConfig {
	return defaultConfig
}

// Load returns the current runtime config.
func Load() *RuntimeConfig {
	if cfg := globalConfig.Load(); cfg != nil {
		return cfg
	}
	return defaultConfig
}

// Store replaces the current runtime config.
func Store(cfg *RuntimeConfig) {
	if cfg == nil {
		cfg = defaultConfig
	}
	globalConfig.Store(cfg)
}

// CompareAndSwap replaces old with next only when old still owns the global config slot.
func CompareAndSwap(old, next *RuntimeConfig) bool {
	if next == nil {
		next = defaultConfig
	}
	return globalConfig.CompareAndSwap(old, next)
}

// MaxContextLength returns the effective byte limit for payload-like text fields.
func (c *RuntimeConfig) MaxContextLength() int {
	if c == nil {
		return DefaultMaxContextLength
	}
	return c.maxContextLength
}

// RecordPayloads reports whether payload-like text should be exported.
func (c *RuntimeConfig) RecordPayloads() bool {
	if c == nil {
		return false
	}
	return c.recordPayloads
}
