// Package genaitest provides test helpers for GenAI runtime state.
package genaitest

import (
	"testing"

	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/internal/defaultslot"
)

// InstallDefaultTrackerForTest swaps the package default GenAI tracker for one test.
// When tracker is nil, it clears the default slot so package helpers fall back to the production noop tracker.
func InstallDefaultTrackerForTest(t testing.TB, tracker *genai.Tracker) {
	t.Helper()

	previous, token := defaultslot.Swap(tracker)
	t.Cleanup(func() {
		defaultslot.Restore(token, previous)
	})
}
