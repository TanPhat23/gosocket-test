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

// Registry manages registered SyncProvider instances in a thread-safe manner using sync.Map.
type Registry struct {
	providers sync.Map // map[models.IntegrationProvider]SyncProvider
}

// NewRegistry creates a new thread-safe Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register registers a SyncProvider. Returns an error if the provider is nil or already registered.
func (r *Registry) Register(provider SyncProvider) error {
	if provider == nil {
		return ErrProviderNil
	}

	p := provider.Provider()
	if _, loaded := r.providers.LoadOrStore(p, provider); loaded {
		return fmt.Errorf("%w: %s", ErrProviderAlreadyRegistered, p)
	}

	return nil
}

// Get retrieves a SyncProvider by provider enum. Returns ErrProviderNotFound if not found.
func (r *Registry) Get(provider models.IntegrationProvider) (SyncProvider, error) {
	val, exists := r.providers.Load(provider)
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, provider)
	}

	return val.(SyncProvider), nil
}

// List returns all registered SyncProvider instances.
func (r *Registry) List() []SyncProvider {
	var list []SyncProvider
	r.providers.Range(func(_, val any) bool {
		list = append(list, val.(SyncProvider))
		return true
	})
	return list
}
