// Package diagnostic bounds library-owned diagnostic text before telemetry fanout.
package diagnostic

import (
	"errors"
	"fmt"
	"runtime/debug"
	"unicode/utf8"
)

// Channel identifies a library-owned diagnostic text channel.
type Channel string

const (
	ChannelErrorMessage        Channel = "error.message"
	ChannelPanicMessage        Channel = "panic.message"
	ChannelExceptionStacktrace Channel = "exception.stacktrace"
	ChannelSecurityReason      Channel = "security.reason"
	MaxInputBytes                      = 65536
	MaxOutputBytes                     = 4096
)

var (
	ErrConfig    = errors.New("diagnostic: invalid configuration")
	ErrInput     = errors.New("diagnostic: invalid input")
	ErrOutput    = errors.New("diagnostic: invalid output")
	ErrSanitizer = errors.New("diagnostic: sanitizer failed")
)

// Config explicitly opts into raw text or host sanitation. Callbacks and arbitrary
// Error/String formatting are host-owned; their execution cannot be bounded here.
type Config struct {
	Raw            bool
	Sanitizer      func(Channel, string) (string, error)
	MaxInputBytes  int
	MaxOutputBytes int
}

// Policy is immutable. Its zero value suppresses arbitrary text without formatting it.
type Policy struct {
	raw           bool
	sanitizer     func(Channel, string) (string, error)
	input, output int
}

// Result contains only bounded text ready for every library-controlled sink.
type Result struct{ Class, Message, Stacktrace string }

// NewPolicy validates limits before installing any host callback.
func NewPolicy(c Config) (Policy, error) {
	if c.MaxInputBytes < 0 || c.MaxInputBytes > MaxInputBytes || c.MaxOutputBytes < 0 ||
		c.MaxOutputBytes > MaxOutputBytes {
		return Policy{}, ErrConfig
	}
	if c.MaxInputBytes == 0 {
		c.MaxInputBytes = MaxInputBytes
	}
	if c.MaxOutputBytes == 0 {
		c.MaxOutputBytes = MaxOutputBytes
	}
	return Policy{raw: c.Raw, sanitizer: c.Sanitizer, input: c.MaxInputBytes, output: c.MaxOutputBytes}, nil
}

// Text validates input before a callback and validates output before fanout.
// Failures contain no input or callback error text.
func (p Policy) Text(
	channel Channel,
	input string,
) (output string, err error) { //nolint:nonamedreturns // recovery must replace both callback outputs.
	if !validChannel(channel) || len(input) > p.inputLimit() || !utf8.ValidString(input) {
		return "", ErrInput
	}
	switch {
	case p.sanitizer != nil:
		defer func() {
			if recover() != nil {
				output = ""
				err = ErrSanitizer
			}
		}()
		output, err = p.sanitizer(channel, input)
		if err != nil {
			return "", ErrSanitizer
		}
	case p.raw:
		output = input
	default:
		return "", nil
	}
	if len(output) > p.outputLimit() || !utf8.ValidString(output) {
		return "", ErrOutput
	}
	return output, nil
}

// Error never calls Error in safe default mode. The original error is not retained.
func (p Policy) Error(err error) Result {
	if err == nil {
		return Result{}
	}
	result := Result{Class: "error", Message: p.fallback("error"), Stacktrace: ""}
	if !p.contentEnabled() {
		return result
	}
	message, ok := formatError(err)
	if !ok {
		result.Message = p.fallback("diagnostic.suppressed")
		return result
	}
	result.Message = p.message(ChannelErrorMessage, message)
	return result
}

// Panic never formats arbitrary objects in safe default mode.
func (p Policy) Panic(value any) Result {
	result := Result{Class: "panic", Message: p.fallback("panic"), Stacktrace: ""}
	if !p.contentEnabled() {
		return result
	}
	message, ok := formatPanic(value)
	if !ok {
		result.Message = p.fallback("diagnostic.suppressed")
		return result
	}
	result.Message = p.message(ChannelPanicMessage, message)
	stack, err := p.Text(ChannelExceptionStacktrace, string(debug.Stack()))
	if err == nil {
		result.Stacktrace = stack
	}
	return result
}

func (p Policy) message(channel Channel, input string) string {
	output, err := p.Text(channel, input)
	if err != nil {
		return p.fallback("diagnostic.suppressed")
	}
	return output
}
func (p Policy) contentEnabled() bool { return p.raw || p.sanitizer != nil }
func (p Policy) inputLimit() int {
	if p.input == 0 {
		return MaxInputBytes
	}
	return p.input
}
func (p Policy) outputLimit() int {
	if p.output == 0 {
		return MaxOutputBytes
	}
	return p.output
}
func validChannel(c Channel) bool {
	return c == ChannelErrorMessage || c == ChannelPanicMessage || c == ChannelExceptionStacktrace ||
		c == ChannelSecurityReason
}
func formatError(err error) (value string, ok bool) { //nolint:nonamedreturns // recovery suppresses formatter panic.
	defer func() {
		if recover() != nil {
			value = ""
			ok = false
		}
	}()
	return err.Error(), true
}
func formatPanic(value any) (text string, ok bool) { //nolint:nonamedreturns // recovery suppresses formatter panic.
	defer func() {
		if recover() != nil {
			text = ""
			ok = false
		}
	}()
	return fmt.Sprint(value), true
}

func (p Policy) fallback(value string) string {
	if len(value) > p.outputLimit() {
		return ""
	}
	return value
}
