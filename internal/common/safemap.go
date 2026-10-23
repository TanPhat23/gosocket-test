package common

import (
	"sync"
)

// SafeMap is a generic thread-safe concurrent map wrapping sync.Map.
type SafeMap[K comparable, V any] struct {
	m sync.Map
}

// NewSafeMap creates a new empty SafeMap instance.
func NewSafeMap[K comparable, V any]() *SafeMap[K, V] {
	return &SafeMap[K, V]{}
}

// Load returns the value stored in the map for a key, or zero value if no value is present.
// The ok result indicates whether value was found in the map.
func (sm *SafeMap[K, V]) Load(key K) (V, bool) {
	val, ok := sm.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return val.(V), true
}

// Store sets the value for a key.
func (sm *SafeMap[K, V]) Store(key K, value V) {
	sm.m.Store(key, value)
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
// The loaded result is true if the value was loaded, false if stored.
func (sm *SafeMap[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	val, loaded := sm.m.LoadOrStore(key, value)
	return val.(V), loaded
}

// LoadAndDelete deletes the value for a key, returning the previous value if any.
// The loaded result reports whether the key was present.
func (sm *SafeMap[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	val, loaded := sm.m.LoadAndDelete(key)
	if !loaded {
		var zero V
		return zero, false
	}
	return val.(V), true
}

// Delete deletes the value for a key.
func (sm *SafeMap[K, V]) Delete(key K) {
	sm.m.Delete(key)
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, range stops the iteration.
func (sm *SafeMap[K, V]) Range(f func(key K, value V) bool) {
	sm.m.Range(func(k, v any) bool {
		return f(k.(K), v.(V))
	})
}
