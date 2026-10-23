package gateway

import (
	"bytes"
	"encoding/json"
)

const (
	maxSSELine    = 1 << 20
	maxJSONBuffer = 8 << 20
)

// usageScanner extracts the provider-reported token usage from a response as it streams past. It never
// stores prompts or completions beyond what it needs to find the usage object.
type usageScanner struct {
	sse       bool
	partial   []byte
	skipLine  bool
	whole     bytes.Buffer
	wholeOver bool
	in, out   int64
	found     bool
}

func newUsageScanner(sse bool) *usageScanner { return &usageScanner{sse: sse} }

func (u *usageScanner) write(p []byte) {
	if !u.sse {
		if u.wholeOver {
			return
		}
		if u.whole.Len()+len(p) > maxJSONBuffer {
			u.wholeOver = true
			u.whole.Reset()
			return
		}
		u.whole.Write(p)
		return
	}
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			u.appendPartial(p)
			return
		}
		u.appendPartial(p[:i])
		u.line()
		p = p[i+1:]
	}
}

func (u *usageScanner) appendPartial(p []byte) {
	if u.skipLine {
		return
	}
	if len(u.partial)+len(p) > maxSSELine {
		u.skipLine = true
		u.partial = u.partial[:0]
		return
	}
	u.partial = append(u.partial, p...)
}

func (u *usageScanner) line() {
	defer func() { u.partial = u.partial[:0]; u.skipLine = false }()
	if u.skipLine {
		return
	}
	l := bytes.TrimSpace(u.partial)
	data, ok := bytes.CutPrefix(l, []byte("data:"))
	if !ok || !bytes.Contains(data, []byte(`"usage"`)) {
		return
	}
	u.parse(bytes.TrimSpace(data))
}

type reportedUsage struct {
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		InputTokens      int64 `json:"input_tokens"`
		OutputTokens     int64 `json:"output_tokens"`
	} `json:"usage"`
}

func (u *usageScanner) parse(b []byte) {
	var r reportedUsage
	if json.Unmarshal(b, &r) != nil || r.Usage == nil {
		return
	}
	in, out := r.Usage.PromptTokens+r.Usage.InputTokens, r.Usage.CompletionTokens+r.Usage.OutputTokens
	if in < 0 || out < 0 || in+out == 0 {
		return
	}
	u.in, u.out, u.found = in, out, true
}

// usage returns the last reported usage, if any was seen.
func (u *usageScanner) usage() (in, out int64, ok bool) {
	if u.sse && len(u.partial) > 0 {
		u.line() // a final event with no trailing newline
	}
	if !u.sse && !u.wholeOver {
		u.parse(u.whole.Bytes())
	}
	return u.in, u.out, u.found
}
