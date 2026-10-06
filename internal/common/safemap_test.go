package common_test

import (
	"sync"
	"testing"

	"agent-task-manager/internal/common"
)

func TestSafeMapBasicOperations(t *testing.T) {
	sm := common.NewSafeMap[string, int]()

	// Initial load should be false
	val, ok := sm.Load("foo")
	if ok || val != 0 {
		t.Fatalf("expected foo not found, got ok=%v, val=%v", ok, val)
	}

	// Store and load
	sm.Store("foo", 42)
	val, ok = sm.Load("foo")
	if !ok || val != 42 {
		t.Fatalf("expected foo=42, got ok=%v, val=%v", ok, val)
	}

	// LoadOrStore existing
	actual, loaded := sm.LoadOrStore("foo", 100)
	if !loaded || actual != 42 {
		t.Fatalf("expected loaded=true, actual=42, got loaded=%v, actual=%v", loaded, actual)
	}

	// LoadOrStore new
	actual, loaded = sm.LoadOrStore("bar", 99)
	if loaded || actual != 99 {
		t.Fatalf("expected loaded=false, actual=99, got loaded=%v, actual=%v", loaded, actual)
	}

	// Delete
	sm.Delete("foo")
	_, ok = sm.Load("foo")
	if ok {
		t.Fatalf("expected foo to be deleted")
	}

	// Range
	var count int
	sm.Range(func(key string, value int) bool {
		count++
		if key != "bar" || value != 99 {
			t.Errorf("unexpected key/val in range: %s=%d", key, value)
		}
		return true
	})
	if count != 1 {
		t.Fatalf("expected 1 element in range, got %d", count)
	}
}

func TestSafeMapConcurrentAccess(t *testing.T) {
	sm := common.NewSafeMap[int, int]()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sm.Store(n, n*2)
			val, ok := sm.Load(n)
			if !ok || val != n*2 {
				t.Errorf("concurrent load mismatch for %d: ok=%v, val=%v", n, ok, val)
			}
		}(i)
	}

	wg.Wait()
}
