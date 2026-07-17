package usecase

import (
	"fmt"
	"sync"
)

const (
	// NameTicketing is the canonical name of the ticketing use case: the
	// existing ProcessTicket dispatch pipeline (clone, branch, commit,
	// push, open a merge request).
	NameTicketing = "ticketing"
	// NameIncidentTriage is the canonical name of the incident-triage use
	// case: the existing ProcessIncidentEvent dispatch pipeline (no
	// repository, no merge request, single engine, no approval gates).
	NameIncidentTriage = "incident-triage"
)

// Registry holds registered use-case Definitions, keyed by name. It is
// expected to be populated once at startup and read many times
// thereafter, so the mutex only needs to guard against concurrent
// registration, not concurrent reads racing with registration.
type Registry struct {
	mu          sync.RWMutex
	definitions map[string]*Definition
}

// NewRegistry returns an empty Registry ready for Register calls.
func NewRegistry() *Registry {
	return &Registry{
		definitions: make(map[string]*Definition),
	}
}

// Register adds def to the registry under def.Name, replacing any
// existing Definition with the same name. It returns an error if def is
// nil or def.Name is empty.
func (reg *Registry) Register(def *Definition) error {
	if def == nil {
		return fmt.Errorf("usecase: cannot register a nil definition")
	}
	if def.Name == "" {
		return fmt.Errorf("usecase: cannot register a definition with an empty name")
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.definitions[def.Name] = def
	return nil
}

// Get returns the Definition registered under name, and whether one was
// found.
func (reg *Registry) Get(name string) (*Definition, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	def, ok := reg.definitions[name]
	return def, ok
}
