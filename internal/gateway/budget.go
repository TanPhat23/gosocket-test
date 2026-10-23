package gateway

import (
	"errors"
	"sync"
)

// ErrQuotaExceeded means the attempt has no remaining request or token budget.
var ErrQuotaExceeded = errors.New("gateway: attempt quota exceeded")

// Quota is a per-attempt ceiling. A zero value rejects everything (fail closed).
type Quota struct {
	MaxRequests int64
	MaxTokens   int64
	// MaxMicroUSD is the spend cap in micro-USD. Only ReserveSpend charges against it; Reserve charges 0.
	MaxMicroUSD int64
}

type usage struct{ requests, tokens, micro int64 }

// Ledger reserves an upper bound of tokens and spend per attempt before a request is dispatched upstream.
// Request and token reservations are never refunded. Spend reservations are settled to the provider's
// reported usage when it is known (Reservation.Settle) and otherwise stay at the conservative bound.
type Ledger struct {
	quota Quota
	mu    sync.Mutex
	used  map[string]usage
}

func NewLedger(q Quota) *Ledger { return &Ledger{quota: q, used: map[string]usage{}} }

// Reserve atomically accounts one request of at most tokens tokens and no spend.
func (l *Ledger) Reserve(attemptID string, tokens int64) error {
	_, err := l.ReserveSpend(attemptID, tokens, 0)
	return err
}

// Reservation is a spend reservation that can be settled once.
type Reservation struct {
	l     *Ledger
	id    string
	micro int64
	once  sync.Once
}

// ReserveSpend atomically accounts one request of at most tokens tokens and at most micro micro-USD.
// All of the request, token and spend ceilings are checked together; nothing is charged on rejection.
func (l *Ledger) ReserveSpend(attemptID string, tokens, micro int64) (*Reservation, error) {
	if attemptID == "" || tokens <= 0 || micro < 0 {
		return nil, errors.New("gateway: invalid reservation")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	u := l.used[attemptID]
	if u.requests+1 > l.quota.MaxRequests || u.tokens+tokens > l.quota.MaxTokens || u.micro+micro > l.quota.MaxMicroUSD {
		return nil, ErrQuotaExceeded
	}
	l.used[attemptID] = usage{u.requests + 1, u.tokens + tokens, u.micro + micro}
	return &Reservation{l: l, id: attemptID, micro: micro}, nil
}

// Settle replaces the reserved spend with the actual spend. Unused reservation is released; an actual
// above the reservation (a provider that exceeded its bound) is charged in full. It is a no-op after
// the first call or once the attempt has been forgotten.
func (r *Reservation) Settle(actualMicro int64) {
	r.once.Do(func() {
		if actualMicro < 0 {
			return
		}
		r.l.mu.Lock()
		defer r.l.mu.Unlock()
		u, ok := r.l.used[r.id]
		if !ok {
			return
		}
		u.micro += actualMicro - r.micro
		if u.micro < 0 {
			u.micro = 0
		}
		r.l.used[r.id] = u
	})
}

// Forget drops an attempt's accounting once it is finished.
func (l *Ledger) Forget(attemptID string) {
	l.mu.Lock()
	delete(l.used, attemptID)
	l.mu.Unlock()
}
