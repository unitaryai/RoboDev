package routing

import (
	"context"
	"fmt"
	"sync"
)

// FingerprintStore persists and retrieves engine fingerprints.
type FingerprintStore interface {
	// Save persists the given engine fingerprint, overwriting any existing
	// record for the same engine.
	Save(ctx context.Context, fp *EngineFingerprint) error

	// Get retrieves the fingerprint for the named engine. Returns an error
	// if no fingerprint exists.
	Get(ctx context.Context, engineName string) (*EngineFingerprint, error)

	// List returns all stored fingerprints.
	List(ctx context.Context) ([]*EngineFingerprint, error)
}

// MemoryFingerprintStore is a thread-safe in-memory implementation of
// FingerprintStore, suitable for development and testing.
type MemoryFingerprintStore struct {
	mu    sync.RWMutex
	store map[string]*EngineFingerprint
}

// NewMemoryFingerprintStore creates an empty in-memory fingerprint store.
func NewMemoryFingerprintStore() *MemoryFingerprintStore {
	return &MemoryFingerprintStore{
		store: make(map[string]*EngineFingerprint),
	}
}

// Save persists the fingerprint, keyed by engine name.
func (m *MemoryFingerprintStore) Save(_ context.Context, fp *EngineFingerprint) error {
	if fp == nil {
		return fmt.Errorf("cannot save nil fingerprint")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[fp.EngineName] = fp
	return nil
}

// Get retrieves the fingerprint for the given engine. Returns an error if
// not found.
func (m *MemoryFingerprintStore) Get(_ context.Context, engineName string) (*EngineFingerprint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	fp, ok := m.store[engineName]
	if !ok {
		return nil, fmt.Errorf("fingerprint for engine %q not found", engineName)
	}
	return fp, nil
}

// List returns all stored fingerprints in no guaranteed order.
func (m *MemoryFingerprintStore) List(_ context.Context) ([]*EngineFingerprint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*EngineFingerprint, 0, len(m.store))
	for _, fp := range m.store {
		result = append(result, fp)
	}
	return result, nil
}
