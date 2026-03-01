// Package taskrun provides the TaskRunStore interface and in-memory
// implementation for persisting TaskRun records.
package taskrun

import (
	"context"
	"fmt"
	"sync"
)

// TaskRunStore is the interface for persisting and querying TaskRun records.
// The default implementation is MemoryStore; future backends (SQLite,
// Postgres) can implement this interface for durable storage.
type TaskRunStore interface {
	// Save persists a TaskRun, creating or updating it by ID.
	Save(ctx context.Context, tr *TaskRun) error

	// Get retrieves a TaskRun by its unique ID. Returns an error if the
	// TaskRun does not exist.
	Get(ctx context.Context, id string) (*TaskRun, error)

	// List returns all stored TaskRuns.
	List(ctx context.Context) ([]*TaskRun, error)

	// ListByTicketID returns all TaskRuns associated with the given ticket.
	ListByTicketID(ctx context.Context, ticketID string) ([]*TaskRun, error)
}

// MemoryStore is a thread-safe in-memory implementation of TaskRunStore.
type MemoryStore struct {
	mu    sync.RWMutex
	store map[string]*TaskRun
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		store: make(map[string]*TaskRun),
	}
}

// Save persists a TaskRun by its ID, overwriting any previous record.
func (m *MemoryStore) Save(_ context.Context, tr *TaskRun) error {
	if tr == nil {
		return fmt.Errorf("cannot save nil task run")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[tr.ID] = tr
	return nil
}

// Get retrieves a TaskRun by ID. Returns an error if not found.
func (m *MemoryStore) Get(_ context.Context, id string) (*TaskRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tr, ok := m.store[id]
	if !ok {
		return nil, fmt.Errorf("task run %q not found", id)
	}
	return tr, nil
}

// List returns all stored TaskRuns in no guaranteed order.
func (m *MemoryStore) List(_ context.Context) ([]*TaskRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*TaskRun, 0, len(m.store))
	for _, tr := range m.store {
		result = append(result, tr)
	}
	return result, nil
}

// ListByTicketID returns all TaskRuns with the given ticket ID.
func (m *MemoryStore) ListByTicketID(_ context.Context, ticketID string) ([]*TaskRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*TaskRun
	for _, tr := range m.store {
		if tr.TicketID == ticketID {
			result = append(result, tr)
		}
	}
	return result, nil
}
