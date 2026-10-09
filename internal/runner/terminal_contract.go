package runner

import (
	"errors"
)

// TerminalTracker monitors OpenCode stream step bracketing and continuation reasons.
// Directly ported from Multica's fail-closed agent completion contract.
type TerminalTracker struct {
	openStep             bool
	awaitingContinuation bool
	sawTerminalSignal    bool
	sawAnyStep           bool
}

// NewTerminalTracker creates a new terminal lifecycle tracker.
func NewTerminalTracker() *TerminalTracker {
	return &TerminalTracker{}
}

// Process updates tracker state from incoming OpenCode wire events.
func (t *TerminalTracker) Process(evt *OpenCodeWireEvent) {
	if evt == nil {
		return
	}

	switch evt.Type {
	case "step_start":
		t.openStep = true
		t.awaitingContinuation = false
		t.sawAnyStep = true

	case "step_finish":
		t.openStep = false
		if evt.Part.Reason == "tool-calls" {
			t.awaitingContinuation = true
		} else if evt.Part.Reason == "stop" || evt.Part.Reason == "end_turn" {
			t.awaitingContinuation = false
			t.sawTerminalSignal = true
		}
	}
}

// CheckCompletion evaluates whether the stream reached a provable, clean terminal state.
func (t *TerminalTracker) CheckCompletion() error {
	if t.openStep {
		return errors.New("stream terminated prematurely while step was still executing")
	}
	if t.awaitingContinuation {
		return errors.New("stream terminated prematurely while model tool calls were pending")
	}
	if !t.sawTerminalSignal && t.sawAnyStep {
		return errors.New("stream ended without terminal stop signal from model")
	}
	return nil
}
