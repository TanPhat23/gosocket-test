package runner

import (
	"strings"
	"sync"
	"testing"
)

func TestANSIBuilder_BasicMethods(t *testing.T) {
	b := AcquireANSIBuilder()
	defer b.Release()

	b.Yellow("warn").Text(" ").Green("ok").CRLF()
	got := b.String()

	expected := "\x1b[33mwarn\x1b[0m \x1b[32mok\x1b[0m\r\n"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}

	if b.Len() != len(expected) {
		t.Fatalf("expected length %d, got %d", len(expected), b.Len())
	}
}

func TestANSIBuilder_DomainMethods(t *testing.T) {
	b := AcquireANSIBuilder()
	defer b.Release()

	b.ToolHeader("shell", "npm test")
	b.ToolSuccess("shell")
	b.ToolFailure("shell")
	b.AgentHeader("coordinator")
	b.AgentError("syntax error")

	res := b.String()
	if !strings.Contains(res, "→ [shell]") || !strings.Contains(res, "npm test") {
		t.Errorf("ToolHeader failed: %q", res)
	}
	if !strings.Contains(res, "✔ Completed tool: shell") {
		t.Errorf("ToolSuccess failed: %q", res)
	}
	if !strings.Contains(res, "✖ Tool failed: shell") {
		t.Errorf("ToolFailure failed: %q", res)
	}
	if !strings.Contains(res, "> coordinator") {
		t.Errorf("AgentHeader failed: %q", res)
	}
	if !strings.Contains(res, "✖ Agent error: syntax error") {
		t.Errorf("AgentError failed: %q", res)
	}
}

func TestANSIBuilder_TruncatedLines(t *testing.T) {
	// Case 1: <= 4 lines
	b1 := AcquireANSIBuilder()
	b1.TruncatedLines("line1\nline2\nline3\n", 4)
	out1 := b1.String()
	b1.Release()

	if !strings.Contains(out1, "line1") || !strings.Contains(out1, "line3") {
		t.Fatalf("expected line1 and line3 in output: %q", out1)
	}
	if strings.Contains(out1, "...") {
		t.Fatalf("expected no ellipsis for 3 lines, got: %q", out1)
	}

	// Case 2: > 4 lines
	b2 := AcquireANSIBuilder()
	b2.TruncatedLines("1\n2\n3\n4\n5\n6\n7\n", 4)
	out2 := b2.String()
	b2.Release()

	if !strings.Contains(out2, "... (7 lines)") {
		t.Fatalf("expected '... (7 lines)' in output, got: %q", out2)
	}
}

func TestANSIBuilder_ConcurrentPool(t *testing.T) {
	var wg sync.WaitGroup
	const workers = 20
	const iterations = 100

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				b := AcquireANSIBuilder()
				b.Cyan("hello").Text(" ").Red("world").CRLF()
				str := b.String()
				if !strings.Contains(str, "hello") || !strings.Contains(str, "world") {
					t.Errorf("unexpected string: %q", str)
				}
				b.Release()
			}
		}()
	}
	wg.Wait()
}

func TestANSIBuilder_ColorGolden(t *testing.T) {
	b := AcquireANSIBuilder()
	defer b.Release()
	b.Yellow("y").Green("g").Red("r").Cyan("c").Yellow("")
	want := "\x1b[33my\x1b[0m\x1b[32mg\x1b[0m\x1b[31mr\x1b[0m\x1b[36mc\x1b[0m\x1b[33m\x1b[0m"
	if got := b.String(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
