package diagnostic

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type hostileError struct{ calls *int }

func (e hostileError) Error() string { *e.calls++; panic("secret") }

type hostilePanic struct{ calls *int }

func (e hostilePanic) String() string { *e.calls++; return "secret" }

func TestDefaultDoesNotFormat(t *testing.T) {
	calls := 0
	p := Policy{}
	require.Equal(t, Result{Class: "error", Message: "error"}, p.Error(hostileError{&calls}))
	require.Equal(t, Result{Class: "panic", Message: "panic"}, p.Panic(hostilePanic{&calls}))
	require.Zero(t, calls)
}
func TestBoundsBeforeCallbackAndBoundedOutput(t *testing.T) {
	calls := 0
	p, err := NewPolicy(
		Config{
			MaxInputBytes:  8,
			MaxOutputBytes: 4,
			Sanitizer:      func(Channel, string) (string, error) { calls++; return "safe", nil },
		},
	)
	require.NoError(t, err)
	for _, input := range []string{strings.Repeat("x", 9), string([]byte{0xff})} {
		_, err = p.Text(ChannelSecurityReason, input)
		require.ErrorIs(t, err, ErrInput)
	}
	require.Zero(t, calls)
	output, err := p.Text(ChannelSecurityReason, "secret")
	require.NoError(t, err)
	require.Equal(t, "safe", output)
	require.Equal(t, 1, calls)
	for _, output := range []string{"large", string([]byte{0xff})} {
		p, err = NewPolicy(
			Config{MaxOutputBytes: 4, Sanitizer: func(Channel, string) (string, error) { return output, nil }},
		)
		require.NoError(t, err)
		_, err = p.Text(ChannelSecurityReason, "secret")
		require.ErrorIs(t, err, ErrOutput)
	}
}
func TestFailuresAreCategorical(t *testing.T) {
	for _, fn := range []func(Channel, string) (string, error){func(Channel, string) (string, error) { return "secret", errors.New("secret") }, func(Channel, string) (string, error) { panic("secret") }} {
		p, err := NewPolicy(Config{Sanitizer: fn})
		require.NoError(t, err)
		require.Equal(t, Result{Class: "error", Message: "diagnostic.suppressed"}, p.Error(errors.New("secret")))
		require.Equal(t, Result{Class: "panic", Message: "diagnostic.suppressed"}, p.Panic("secret"))
	}
}
func TestRawAndInvalidConfig(t *testing.T) {
	p, err := NewPolicy(Config{Raw: true})
	require.NoError(t, err)
	require.Equal(t, "secret", p.Error(errors.New("secret")).Message)
	for _, c := range []Config{{MaxInputBytes: -1}, {MaxInputBytes: MaxInputBytes + 1}, {MaxOutputBytes: -1}, {MaxOutputBytes: MaxOutputBytes + 1}} {
		_, err = NewPolicy(c)
		require.ErrorIs(t, err, ErrConfig)
	}
}

func TestFallbackRespectsSmallOutputBudget(t *testing.T) {
	for _, config := range []Config{{MaxOutputBytes: 1}, {MaxOutputBytes: 1, Sanitizer: func(Channel, string) (string, error) { return "", ErrSanitizer }}} {
		policy, err := NewPolicy(config)
		require.NoError(t, err)
		require.Empty(t, policy.Error(errors.New("secret")).Message)
		require.Empty(t, policy.Panic("secret").Message)
	}
}
