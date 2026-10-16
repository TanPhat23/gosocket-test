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
}

type usage struct{ requests, tokens int64 }

// Ledger reserves an upper bound of tokens per attempt before a request is dispatched upstream.
// Reservations are never refunded: the quota is a conservative ceiling on what was authorised,
// not a reconciliation of what was billed.
type Ledger struct {
	quota Quota
	mu    sync.Mutex
	used  map[string]usage
}

func NewLedger(q Quota) *Ledger { return &Ledger{quota: q, used: map[string]usage{}} }

// Reserve atomically accounts one request of at most tokens tokens.
func (l *Ledger) Reserve(attemptID string, tokens int64) error {
	if attemptID == "" || tokens <= 0 {
		return errors.New("gateway: invalid reservation")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	u := l.used[attemptID]
	if u.requests+1 > l.quota.MaxRequests || u.tokens+tokens > l.quota.MaxTokens {
		return ErrQuotaExceeded
	}
	l.used[attemptID] = usage{u.requests + 1, u.tokens + tokens}
	return nil
}

// Forget drops an attempt's accounting once it is finished.
func (l *Ledger) Forget(attemptID string) {
	l.mu.Lock()
	delete(l.used, attemptID)
	l.mu.Unlock()
}
