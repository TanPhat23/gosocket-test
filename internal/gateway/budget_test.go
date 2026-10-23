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

func TestLedger_SpendCapRejectsAndChargesNothing(t *testing.T) {
	l := NewLedger(Quota{MaxRequests: 10, MaxTokens: 1000, MaxMicroUSD: 100})
	if _, err := l.ReserveSpend("a", 10, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ReserveSpend("a", 10, 41); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("60+41>100 must be rejected, got %v", err)
	}
	if got := l.used["a"]; got != (usage{1, 10, 60}) {
		t.Errorf("a rejected reservation must charge nothing, got %+v", got)
	}
	if _, err := l.ReserveSpend("a", 10, 40); err != nil {
		t.Errorf("exactly reaching the cap must be allowed: %v", err)
	}
}

func TestLedger_SettleRefundsUnusedAndChargesOverage(t *testing.T) {
	l := NewLedger(Quota{MaxRequests: 10, MaxTokens: 1000, MaxMicroUSD: 100})
	r, _ := l.ReserveSpend("a", 10, 80)
	r.Settle(30)
	if got := l.used["a"].micro; got != 30 {
		t.Errorf("settle must release the unused 50, left %d", got)
	}
	r.Settle(0) // second settle is a no-op
	if got := l.used["a"].micro; got != 30 {
		t.Errorf("settle must apply once, got %d", got)
	}
	over, _ := l.ReserveSpend("a", 10, 10)
	over.Settle(25) // a provider that exceeded its bound is charged in full
	if got := l.used["a"].micro; got != 30+25 {
		t.Errorf("overage must be charged, got %d", got)
	}
	// The refund is usable by the next request.
	if _, err := l.ReserveSpend("a", 10, 45); err != nil {
		t.Errorf("refunded spend must be reservable: %v", err)
	}
}

func TestLedger_SettleAfterForgetDoesNotResurrect(t *testing.T) {
	l := NewLedger(Quota{MaxRequests: 10, MaxTokens: 1000, MaxMicroUSD: 100})
	r, _ := l.ReserveSpend("a", 10, 50)
	l.Forget("a")
	r.Settle(10)
	if _, ok := l.used["a"]; ok {
		t.Error("settling a forgotten attempt must not recreate it")
	}
}

func TestLedger_ConcurrentSpendNeverOvershoots(t *testing.T) {
	l := NewLedger(Quota{MaxRequests: 1000, MaxTokens: 1_000_000, MaxMicroUSD: 1000})
	var ok atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := l.ReserveSpend("a", 1, 10); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 100 {
		t.Errorf("exactly 100 reservations of 10 fit a cap of 1000, got %d", ok.Load())
	}
}
