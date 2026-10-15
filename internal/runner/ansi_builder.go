package runner

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
)

const (
	ansiReset   = "\x1b[0m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
	ansiGray    = "\x1b[90m"
	ansiCRLF    = "\r\n"

	ansiMaxPooledBufferBytes = 16 * 1024 // 16 KiB
)

var ansiBuilderPool = sync.Pool{
	New: func() any {
		return &ANSIBuilder{
			buf: bytes.NewBuffer(make([]byte, 0, 512)),
		}
	},
}

// ANSIBuilder provides a chainable, zero-allocation fluent builder for formatted terminal strings.
// Instances are acquired from a sync.Pool via AcquireANSIBuilder() and returned with b.Release().
type ANSIBuilder struct {
	buf *bytes.Buffer
}

// AcquireANSIBuilder retrieves a clean ANSIBuilder from the pool.
func AcquireANSIBuilder() *ANSIBuilder {
	b := ansiBuilderPool.Get().(*ANSIBuilder)
	b.buf.Reset()
	return b
}

// Release returns the builder to the sync.Pool if within the maximum buffer capacity.
func (b *ANSIBuilder) Release() {
	if b == nil || b.buf == nil {
		return
	}
	if b.buf.Cap() <= ansiMaxPooledBufferBytes {
		ansiBuilderPool.Put(b)
	}
}

// Text appends plain unstyled text.
func (b *ANSIBuilder) Text(s string) *ANSIBuilder {
	b.buf.WriteString(s)
	return b
}

// CRLF appends a carriage return and newline (\r\n).
func (b *ANSIBuilder) CRLF() *ANSIBuilder {
	b.buf.WriteString(ansiCRLF)
	return b
}

// color appends s wrapped in the given ANSI color code, followed by a reset.
func (b *ANSIBuilder) color(code, s string) *ANSIBuilder {
	b.buf.WriteString(code)
	b.buf.WriteString(s)
	b.buf.WriteString(ansiReset)
	return b
}

// Yellow appends yellow-colored text and resets styling.
func (b *ANSIBuilder) Yellow(s string) *ANSIBuilder {
	return b.color(ansiYellow, s)
}

// Green appends green-colored text and resets styling.
func (b *ANSIBuilder) Green(s string) *ANSIBuilder {
	return b.color(ansiGreen, s)
}

// Red appends red-colored text and resets styling.
func (b *ANSIBuilder) Red(s string) *ANSIBuilder {
	return b.color(ansiRed, s)
}

// Cyan appends cyan-colored text and resets styling.
func (b *ANSIBuilder) Cyan(s string) *ANSIBuilder {
	return b.color(ansiCyan, s)
}

// ToolHeader appends the tool indicator "→ [toolName]" with optional gray command.
func (b *ANSIBuilder) ToolHeader(toolName, cmd string) *ANSIBuilder {
	b.buf.WriteString(ansiYellow)
	b.buf.WriteString("→ [")
	b.buf.WriteString(toolName)
	b.buf.WriteString("]")
	b.buf.WriteString(ansiReset)

	if cmd != "" {
		b.buf.WriteString(" ")
		b.buf.WriteString(ansiGray)
		b.buf.WriteString(cmd)
		b.buf.WriteString(ansiReset)
	}
	b.buf.WriteString(ansiCRLF)
	return b
}

// ToolSuccess appends "✔ Completed tool: <toolName>".
func (b *ANSIBuilder) ToolSuccess(toolName string) *ANSIBuilder {
	b.buf.WriteString(ansiGreen)
	b.buf.WriteString("✔ Completed tool: ")
	b.buf.WriteString(toolName)
	b.buf.WriteString(ansiReset)
	b.buf.WriteString(ansiCRLF)
	return b
}

// ToolFailure appends "✖ Tool failed: <toolName>".
func (b *ANSIBuilder) ToolFailure(toolName string) *ANSIBuilder {
	b.buf.WriteString(ansiRed)
	b.buf.WriteString("✖ Tool failed: ")
	b.buf.WriteString(toolName)
	b.buf.WriteString(ansiReset)
	b.buf.WriteString(ansiCRLF)
	return b
}

// AgentHeader appends "\r\n> <agentName>\r\n".
func (b *ANSIBuilder) AgentHeader(agentName string) *ANSIBuilder {
	b.buf.WriteString(ansiCRLF)
	b.buf.WriteString(ansiCyan)
	b.buf.WriteString("> ")
	b.buf.WriteString(agentName)
	b.buf.WriteString(ansiReset)
	b.buf.WriteString(ansiCRLF)
	return b
}

// SubagentDispatch appends "[subagent: <agent>] ⚡ Dispatched: <desc>".
func (b *ANSIBuilder) SubagentDispatch(agent, desc string) *ANSIBuilder {
	b.buf.WriteString(ansiMagenta)
	b.buf.WriteString("[subagent: ")
	b.buf.WriteString(agent)
	b.buf.WriteString("]")
	b.buf.WriteString(ansiReset)
	b.buf.WriteString(" ")
	b.buf.WriteString(ansiYellow)
	b.buf.WriteString("⚡ Dispatched:")
	b.buf.WriteString(ansiReset)
	if desc != "" {
		b.buf.WriteString(" ")
		b.buf.WriteString(ansiGray)
		b.buf.WriteString(desc)
		b.buf.WriteString(ansiReset)
	}
	b.buf.WriteString(ansiCRLF)
	return b
}

// SubagentSuccess appends "[subagent: <agent>] ✔ Completed task".
func (b *ANSIBuilder) SubagentSuccess(agent string) *ANSIBuilder {
	b.buf.WriteString(ansiMagenta)
	b.buf.WriteString("[subagent: ")
	b.buf.WriteString(agent)
	b.buf.WriteString("]")
	b.buf.WriteString(ansiReset)
	b.buf.WriteString(" ")
	b.buf.WriteString(ansiGreen)
	b.buf.WriteString("✔ Completed task")
	b.buf.WriteString(ansiReset)
	b.buf.WriteString(ansiCRLF)
	return b
}

// SubagentFailure appends "[subagent: <agent>] ✖ Failed".
func (b *ANSIBuilder) SubagentFailure(agent string) *ANSIBuilder {
	b.buf.WriteString(ansiMagenta)
	b.buf.WriteString("[subagent: ")
	b.buf.WriteString(agent)
	b.buf.WriteString("]")
	b.buf.WriteString(ansiReset)
	b.buf.WriteString(" ")
	b.buf.WriteString(ansiRed)
	b.buf.WriteString("✖ Failed")
	b.buf.WriteString(ansiReset)
	b.buf.WriteString(ansiCRLF)
	return b
}

// AgentError appends "\r\n✖ Agent error: <errMsg>\r\n".
func (b *ANSIBuilder) AgentError(errMsg string) *ANSIBuilder {
	b.buf.WriteString(ansiCRLF)
	b.buf.WriteString(ansiRed)
	b.buf.WriteString("✖ Agent error: ")
	b.buf.WriteString(errMsg)
	b.buf.WriteString(ansiReset)
	b.buf.WriteString(ansiCRLF)
	return b
}

// TruncatedLines scans multi-line output line-by-line without slice allocations,
// writing up to maxLines indented in gray, and an ellipsis for any remaining lines.
func (b *ANSIBuilder) TruncatedLines(text string, maxLines int) *ANSIBuilder {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return b
	}
	rem := trimmed
	linesPrinted := 0
	totalLines := strings.Count(trimmed, "\n") + 1

	for len(rem) > 0 && linesPrinted < maxLines {
		idx := strings.IndexByte(rem, '\n')
		var line string
		if idx >= 0 {
			line = rem[:idx]
			rem = rem[idx+1:]
		} else {
			line = rem
			rem = ""
		}
		b.buf.WriteString("  ")
		b.buf.WriteString(ansiGray)
		b.buf.WriteString(line)
		b.buf.WriteString(ansiReset)
		b.buf.WriteString(ansiCRLF)
		linesPrinted++
	}

	if totalLines > maxLines {
		b.buf.WriteString("  ")
		b.buf.WriteString(ansiGray)
		b.buf.WriteString("... (")
		b.buf.WriteString(strconv.Itoa(totalLines))
		b.buf.WriteString(" lines)")
		b.buf.WriteString(ansiReset)
		b.buf.WriteString(ansiCRLF)
	}

	return b
}

// String returns the accumulated formatted text.
func (b *ANSIBuilder) String() string {
	return b.buf.String()
}

// Bytes returns the accumulated formatted bytes.
func (b *ANSIBuilder) Bytes() []byte {
	return b.buf.Bytes()
}

// Len returns the number of bytes written.
func (b *ANSIBuilder) Len() int {
	return b.buf.Len()
}
