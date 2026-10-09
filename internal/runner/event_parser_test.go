package runner

import (
	"testing"
)

func TestParseOpenCodeWireEvent_AllEventTypes(t *testing.T) {
	// 1. step_start
	startJSON := `{"type":"step_start","timestamp":1791452475152,"sessionID":"ses_123","part":{"id":"prt_1","type":"step-start"}}`
	evt, err := ParseOpenCodeWireEvent([]byte(startJSON))
	if err != nil || evt.Type != "step_start" {
		t.Fatalf("failed to parse step_start: %v", err)
	}

	// 2. tool_use
	toolJSON := `{"type":"tool_use","timestamp":1791452475164,"sessionID":"ses_123","part":{"tool":"shell","state":{"status":"completed","input":{"command":"npm test"},"output":"PASS\n"}}}`
	evt, err = ParseOpenCodeWireEvent([]byte(toolJSON))
	if err != nil || evt.Type != "tool_use" || evt.Part.Tool != "shell" {
		t.Fatalf("failed to parse tool_use: %v", err)
	}
	ansi, isLog := FormatWireEventANSI(evt)
	if !isLog || ansi == "" {
		t.Fatalf("expected formatted ANSI log for tool_use, got: %q", ansi)
	}

	// 3. text
	textJSON := `{"type":"text","timestamp":1791452478523,"sessionID":"ses_123","part":{"text":"All tests pass cleanly"}}`
	evt, err = ParseOpenCodeWireEvent([]byte(textJSON))
	if err != nil || evt.Type != "text" || evt.Part.Text != "All tests pass cleanly" {
		t.Fatalf("failed to parse text event: %v", err)
	}

	// 4. step_finish
	finishJSON := `{"type":"step_finish","timestamp":1791452475469,"sessionID":"ses_123","part":{"type":"step-finish","reason":"stop","tokens":{"input":100,"output":50}}}`
	evt, err = ParseOpenCodeWireEvent([]byte(finishJSON))
	if err != nil || evt.Type != "step_finish" || evt.Part.Reason != "stop" {
		t.Fatalf("failed to parse step_finish: %v", err)
	}
}

func BenchmarkFormatWireEventANSI_ToolUse(b *testing.B) {
	evt := &OpenCodeWireEvent{
		Type: "tool_use",
		Part: OpenCodeWirePart{
			Tool: "shell",
			State: &OpenCodeToolState{
				Status: "completed",
				Input: map[string]any{
					"command": "npm run test",
				},
				Output: "PASS src/app.test.ts\nPASS src/utils.test.ts\nTest Suites: 2 passed, 2 total\nTests: 15 passed, 15 total\n",
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = FormatWireEventANSI(evt)
	}
}
