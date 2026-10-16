package gateway

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLedger_ReservesUpToLimit(t *testing.T) {
	l := NewLedger(Quota{MaxRequests: 10, MaxTokens: 100})
	if err := l.Reserve("a", 60); err != nil {
		t.Fatal(err)
	}
	if err := l.Reserve("a", 41); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("expected quota exceeded for 60+41>100, got %v", err)
	}
	if err := l.Reserve("a", 40); err != nil {
		t.Errorf("exactly reaching the limit must be allowed: %v", err)
	}
}

func TestLedger_RequestCountLimit(t *testing.T) {
	l := NewLedger(Quota{MaxRequests: 2, MaxTokens: 1000})
	for i := 0; i < 2; i++ {
		if err := l.Reserve("a", 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Reserve("a", 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("third request: %v", err)
	}
}

func TestLedger_AttemptsAreIndependent(t *testing.T) {
	l := NewLedger(Quota{MaxRequests: 1, MaxTokens: 10})
	if err := l.Reserve("a", 10); err != nil {
		t.Fatal(err)
	}
	if err := l.Reserve("b", 10); err != nil {
		t.Errorf("attempt b blocked by attempt a: %v", err)
	}
}

func TestLedger_FailsClosedOnBadInput(t *testing.T) {
	l := NewLedger(Quota{MaxRequests: 10, MaxTokens: 100})
	for _, n := range []int64{0, -5} {
		if err := l.Reserve("a", n); err == nil {
			t.Errorf("reserve(%d) accepted", n)
		}
	}
	if err := l.Reserve("", 5); err == nil {
		t.Error("empty attempt id accepted")
	}
	if err := NewLedger(Quota{}).Reserve("a", 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("zero quota must reject: %v", err)
	}
}

func TestLedger_ConcurrentReservationsNeverOvershoot(t *testing.T) {
	l := NewLedger(Quota{MaxRequests: 1000, MaxTokens: 100})
	var ok atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Reserve("a", 7) == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := ok.Load() * 7; got > 100 {
		t.Errorf("reserved %d tokens, limit 100", got)
	}
	if ok.Load() != 14 {
		t.Errorf("expected exactly 14 reservations, got %d", ok.Load())
	}
}

func TestLedger_ForgetFreesAttempt(t *testing.T) {
	l := NewLedger(Quota{MaxRequests: 1, MaxTokens: 10})
	_ = l.Reserve("a", 10)
	l.Forget("a")
	if err := l.Reserve("a", 10); err != nil {
		t.Errorf("after Forget: %v", err)
	}
}
