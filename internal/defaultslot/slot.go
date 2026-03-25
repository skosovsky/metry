// Package defaultslot provides an owner-safe slot for package-scoped defaults.
package defaultslot

import "sync/atomic"

// Token is an opaque handle for the currently installed default value.
type Token struct {
	value any
}

var current atomic.Pointer[Token]

// Load returns the current default value, if any.
func Load() any {
	if token := current.Load(); token != nil {
		return token.value
	}
	return nil
}

// Swap installs the next value and returns the previous value plus an ownership token.
func Swap(next any) (any, *Token) {
	nextToken := &Token{value: next}
	previousToken := current.Swap(nextToken)
	if previousToken != nil {
		return previousToken.value, nextToken
	}
	return nil, nextToken
}

// Restore swaps the slot back to previous only when token still owns the slot.
func Restore(token *Token, previous any) bool {
	if token == nil {
		return false
	}
	return current.CompareAndSwap(token, &Token{value: previous})
}
