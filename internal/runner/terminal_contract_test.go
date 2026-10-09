package runner

import (
	"testing"
)

func TestTerminalTracker_FailClosed(t *testing.T) {
	tracker := NewTerminalTracker()

	// 1. Process step start
	tracker.Process(&OpenCodeWireEvent{Type: "step_start"})
	// Clean exit before finish must fail closed
	if err := tracker.CheckCompletion(); err == nil {
		t.Fatalf("expected error for open step without step_finish")
	}

	// 2. Process tool continuation
	tracker.Process(&OpenCodeWireEvent{
		Type: "step_finish",
		Part: OpenCodeWirePart{Reason: "tool-calls"},
	})
	// Exit on tool-calls continuation must fail closed
	if err := tracker.CheckCompletion(); err == nil {
		t.Fatalf("expected error for step finishing with tool-calls pending")
	}

	// 3. Process final stop reason
	tracker.Process(&OpenCodeWireEvent{
		Type: "step_finish",
		Part: OpenCodeWirePart{Reason: "stop"},
	})
	// Now it must succeed
	if err := tracker.CheckCompletion(); err != nil {
		t.Fatalf("expected clean completion after reason: stop, got: %v", err)
	}
}
