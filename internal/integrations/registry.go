package integrations

import (
	"errors"
	"fmt"
	"sync"

	"agent-task-manager/internal/models"
)

var (
	// ErrProviderNotFound is returned when looking up an unregistered provider.
	ErrProviderNotFound = errors.New("provider not registered")
	// ErrProviderNil is returned when attempting to register a nil provider.
	ErrProviderNil = errors.New("cannot register nil provider")
	// ErrProviderAlreadyRegistered is returned when a provider is already registered.
	ErrProviderAlreadyRegistered = errors.New("provider already registered")
)

// Registry manages registered SyncProvider instances in a thread-safe manner.
type Registry struct {
	mu        sync.RWMutex
	providers map[models.IntegrationProvider]SyncProvider
}

// NewRegistry creates a new thread-safe Registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[models.IntegrationProvider]SyncProvider),
	}
}

// Register registers a SyncProvider. Returns an error if the provider is nil or already registered.
func (r *Registry) Register(provider SyncProvider) error {
	if provider == nil {
		return ErrProviderNil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	p := provider.Provider()
	if _, exists := r.providers[p]; exists {
		return fmt.Errorf("%w: %s", ErrProviderAlreadyRegistered, p)
	}

	r.providers[p] = provider
	return nil
}

// Get retrieves a SyncProvider by provider enum. Returns ErrProviderNotFound if not found.
func (r *Registry) Get(provider models.IntegrationProvider) (SyncProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.providers[provider]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, provider)
	}

	return p, nil
}

// List returns all registered SyncProvider instances.
func (r *Registry) List() []SyncProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]SyncProvider, 0, len(r.providers))
	for _, p := range r.providers {
		list = append(list, p)
	}
	return list
}
