package defaultslot

import "testing"

func TestRestore_IsOwnerSafe(t *testing.T) {
	previous, firstToken := Swap("first")
	t.Cleanup(func() {
		Restore(firstToken, previous)
	})

	prevSecond, secondToken := Swap("second")
	if prevSecond != "first" {
		t.Fatalf("expected previous value %q, got %v", "first", prevSecond)
	}

	if Restore(firstToken, previous) {
		t.Fatalf("older owner should not reset a newer value")
	}
	if Load() != "second" {
		t.Fatalf("expected current value %q, got %v", "second", Load())
	}
	if !Restore(secondToken, previous) {
		t.Fatalf("current owner should restore previous value")
	}
}
