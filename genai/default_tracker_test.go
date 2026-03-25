package genai

import (
	"testing"

	"github.com/skosovsky/metry/internal/defaultslot"
)

func installDefaultTrackerForTest(t testing.TB, tracker *Tracker) {
	t.Helper()

	previous, token := defaultslot.Swap(tracker)
	t.Cleanup(func() {
		defaultslot.Restore(token, previous)
	})
}
