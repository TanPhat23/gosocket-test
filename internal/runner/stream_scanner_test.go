package runner

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestAgentStreamScanner_LargeLineCapacity(t *testing.T) {
	// 500 KB single line (exceeds default bufio.MaxScanTokenSize of 64 KB)
	largeLine := strings.Repeat("A", 500*1024) + "\n"

	// Standard scanner should fail with ErrTooLong
	stdScanner := bufio.NewScanner(bytes.NewReader([]byte(largeLine)))
	if stdScanner.Scan() {
		t.Fatalf("expected standard bufio.Scanner to fail on 500KB line, but it succeeded")
	}
	if stdScanner.Err() != bufio.ErrTooLong {
		t.Fatalf("expected ErrTooLong, got: %v", stdScanner.Err())
	}

	// NewAgentStreamScanner must succeed
	agentScanner := NewAgentStreamScanner(bytes.NewReader([]byte(largeLine)))
	if !agentScanner.Scan() {
		t.Fatalf("expected AgentStreamScanner to succeed on 500KB line, got err: %v", agentScanner.Err())
	}
	if len(agentScanner.Text()) != 500*1024 {
		t.Fatalf("expected 500KB line length, got %d", len(agentScanner.Text()))
	}
}
