package runner

import (
	"bufio"
	"io"
	"sync"
)

const (
	// AgentStreamMaxLineBytes bounds a single line read from an agent CLI's event stream (32 MiB).
	// Prevents bufio.ErrTooLong when agents emit massive tool outputs (e.g. read_file of large files or large git diffs).
	AgentStreamMaxLineBytes = 32 * 1024 * 1024

	// AgentStreamInitialBufferBytes is the scanner's initial allocation (1 MiB).
	AgentStreamInitialBufferBytes = 1024 * 1024

	// AgentStreamMaxPooledBufferBytes defines the max capacity of a buffer that can be returned to the pool (2 MiB).
	// Prevents memory bloat from oversized lines expanding the pooled slices.
	AgentStreamMaxPooledBufferBytes = 2 * 1024 * 1024
)

var streamBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, AgentStreamInitialBufferBytes)
		return &b
	},
}

// AgentStreamScanner wraps bufio.Scanner and manages a pooled slice buffer.
type AgentStreamScanner struct {
	*bufio.Scanner
	bufPtr *[]byte
	closed bool
	mu     sync.Mutex
}

// NewAgentStreamScanner returns an AgentStreamScanner sized for high-throughput agent JSON streams.
func NewAgentStreamScanner(r io.Reader) *AgentStreamScanner {
	bufPtr := streamBufferPool.Get().(*[]byte)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(*bufPtr, AgentStreamMaxLineBytes)

	return &AgentStreamScanner{
		Scanner: scanner,
		bufPtr:  bufPtr,
	}
}

// Close releases the scanner's buffer back to streamBufferPool if within AgentStreamMaxPooledBufferBytes.
// Close is thread-safe and idempotent.
func (s *AgentStreamScanner) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true

	if s.bufPtr == nil {
		return
	}

	if cap(*s.bufPtr) <= AgentStreamMaxPooledBufferBytes {
		*s.bufPtr = (*s.bufPtr)[:0]
		streamBufferPool.Put(s.bufPtr)
	}
	s.bufPtr = nil
}
