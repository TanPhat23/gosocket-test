package runner

import (
	"bufio"
	"bytes"
	"strings"
	"sync"
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
	defer agentScanner.Close()
	if !agentScanner.Scan() {
		t.Fatalf("expected AgentStreamScanner to succeed on 500KB line, got err: %v", agentScanner.Err())
	}
	if len(agentScanner.Text()) != 500*1024 {
		t.Fatalf("expected 500KB line length, got %d", len(agentScanner.Text()))
	}
}

func TestAgentStreamScanner_BufferReuse(t *testing.T) {
	// 1. Scanner acquires from pool and Close() returns buffer without panic.
	s1 := NewAgentStreamScanner(strings.NewReader("line1\nline2\n"))
	buf1 := s1.bufPtr.Load()
	if buf1 == nil || cap(*buf1) < AgentStreamInitialBufferBytes {
		t.Fatalf("expected initial buffer capacity >= %d, got %v", AgentStreamInitialBufferBytes, buf1)
	}
	if !s1.Scan() || s1.Text() != "line1" {
		t.Fatalf("unexpected scan result: %v", s1.Text())
	}
	s1.Close()

	// 2. Calling Close() multiple times is idempotent.
	s1.Close()
	s1.Close()
	if s1.bufPtr.Load() != nil {
		t.Fatalf("expected s1.bufPtr to be nil after Close()")
	}

	// 3. Acquire another scanner to see if buffer can be reused from pool.
	s2 := NewAgentStreamScanner(strings.NewReader("reused\n"))
	if s2.bufPtr.Load() == nil {
		t.Fatalf("expected valid bufPtr from pool")
	}
	if !s2.Scan() || s2.Text() != "reused" {
		t.Fatalf("unexpected scan result: %v", s2.Text())
	}
	s2.Close()

	// 4. Buffers exceeding AgentStreamMaxPooledBufferBytes are not retained in the pool.
	// We simulate or provide a buffer exceeding AgentStreamMaxPooledBufferBytes.
	oversizedBuf := make([]byte, 0, AgentStreamMaxPooledBufferBytes+1024)
	sOver := &AgentStreamScanner{
		Scanner: bufio.NewScanner(strings.NewReader("oversized\n")),
	}
	sOver.bufPtr.Store(&oversizedBuf)
	sOver.Buffer(oversizedBuf, AgentStreamMaxLineBytes)
	if !sOver.Scan() || sOver.Text() != "oversized" {
		t.Fatalf("unexpected scan result: %v", sOver.Text())
	}
	overPtr := sOver.bufPtr.Load()
	sOver.Close()

	// Retrieve another buffer from pool. It should NOT be the oversized buffer.
	sNext := NewAgentStreamScanner(strings.NewReader("test\n"))
	if sNext.bufPtr.Load() == overPtr {
		t.Fatalf("oversized buffer was incorrectly returned to the pool")
	}
	sNext.Close()

	// 5. Multiple allocations and reuses work concurrently with -race.
	var wg sync.WaitGroup
	const workers = 32
	const iterations = 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				content := "hello world from worker\nsecond line\n"
				sc := NewAgentStreamScanner(strings.NewReader(content))
				count := 0
				for sc.Scan() {
					count++
				}
				if sc.Err() != nil {
					t.Errorf("scanner error in worker %d iter %d: %v", workerID, j, sc.Err())
				}
				if count != 2 {
					t.Errorf("expected 2 lines in worker %d iter %d, got %d", workerID, j, count)
				}
				sc.Close()
				// test idempotent Close concurrently
				sc.Close()
			}
		}(i)
	}
	wg.Wait()
}

func BenchmarkAgentStreamScanner_Unpooled(b *testing.B) {
	payload := strings.Repeat("{\"type\":\"text\",\"text\":\"streaming chunk data\"}\n", 10)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r := strings.NewReader(payload)
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 0, AgentStreamInitialBufferBytes), AgentStreamMaxLineBytes)
		for s.Scan() {
			_ = s.Bytes()
		}
	}
}

func BenchmarkAgentStreamScanner_Pooled(b *testing.B) {
	payload := strings.Repeat("{\"type\":\"text\",\"text\":\"streaming chunk data\"}\n", 10)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r := strings.NewReader(payload)
		s := NewAgentStreamScanner(r)
		for s.Scan() {
			_ = s.Bytes()
		}
		s.Close()
	}
}

func BenchmarkAgentStreamScanner_Parallel_Unpooled(b *testing.B) {
	payload := strings.Repeat("{\"type\":\"text\",\"text\":\"streaming chunk data\"}\n", 10)
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r := strings.NewReader(payload)
			s := bufio.NewScanner(r)
			s.Buffer(make([]byte, 0, AgentStreamInitialBufferBytes), AgentStreamMaxLineBytes)
			for s.Scan() {
				_ = s.Bytes()
			}
		}
	})
}

func BenchmarkAgentStreamScanner_Parallel_Pooled(b *testing.B) {
	payload := strings.Repeat("{\"type\":\"text\",\"text\":\"streaming chunk data\"}\n", 10)
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r := strings.NewReader(payload)
			s := NewAgentStreamScanner(r)
			for s.Scan() {
				_ = s.Bytes()
			}
			s.Close()
		}
	})
}

func TestAgentStreamScanner_ConcurrentClose(t *testing.T) {
	for i := 0; i < 50; i++ {
		s := NewAgentStreamScanner(strings.NewReader("x\n"))
		var wg sync.WaitGroup
		for g := 0; g < 16; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.Close()
			}()
		}
		wg.Wait()
		if s.bufPtr.Load() != nil {
			t.Fatalf("expected bufPtr to be nil after concurrent Close()")
		}
	}
}
