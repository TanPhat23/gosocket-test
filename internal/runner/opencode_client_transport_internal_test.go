package runner

import (
	"fmt"
	"testing"
	"time"
)

func TestSharedOpenCodeTransport_SameKeyReturnsSameTransport(t *testing.T) {
	key := "test-same|" + t.Name()
	a := sharedOpenCodeTransport(key, nil)
	b := sharedOpenCodeTransport(key, nil)
	if a != b {
		t.Fatal("expected the cached transport for an endpoint to be reused")
	}
}

func TestSharedOpenCodeTransport_DistinctKeysGetDistinctTransports(t *testing.T) {
	a := sharedOpenCodeTransport("test-distinct-a|"+t.Name(), nil)
	b := sharedOpenCodeTransport("test-distinct-b|"+t.Name(), nil)
	if a == b {
		t.Fatal("endpoints must not share a connection pool")
	}
}

func TestSharedOpenCodeTransport_IdleConnectionsExpire(t *testing.T) {
	tr := sharedOpenCodeTransport("test-idle|"+t.Name(), nil)
	if tr.IdleConnTimeout != openCodeIdleConnTimeout || tr.IdleConnTimeout <= 0 || tr.IdleConnTimeout > 5*time.Second {
		t.Fatalf("IdleConnTimeout = %v, want %v (positive and below common server keep-alive)", tr.IdleConnTimeout, openCodeIdleConnTimeout)
	}
}

func TestSharedOpenCodeTransport_CacheIsBounded(t *testing.T) {
	for i := 0; i < openCodeMaxTransports+10; i++ {
		sharedOpenCodeTransport(fmt.Sprintf("test-bound|%s|%d", t.Name(), i), nil)
		openCodeTransports.mu.Lock()
		n := len(openCodeTransports.byKey)
		openCodeTransports.mu.Unlock()
		if n > openCodeMaxTransports {
			t.Fatalf("cache holds %d transports after insert %d, want at most %d", n, i, openCodeMaxTransports)
		}
	}
}
