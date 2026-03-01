// Package noop provides a no-op ticketing backend that silently accepts
// all state transitions and returns empty results. It is used as a fallback
// when no real ticketing backend is configured, preventing nil-pointer
// panics in webhook-only or test deployments.
//
// When configured with a task file path, the backend acts as a file-watcher
// that reads tasks from a local YAML file, enabling local development
// without a real ticketing provider.
package noop

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/unitaryai/robodev/pkg/engine"
	"github.com/unitaryai/robodev/pkg/plugin/ticketing"
)

// FileTask represents a task definition read from a local YAML file.
// It maps directly to a ticketing.Ticket for local development workflows.
type FileTask struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description,omitempty"`
	RepoURL     string   `yaml:"repo_url,omitempty"`
	Labels      []string `yaml:"labels,omitempty"`
}

// Backend is a no-op implementation of the ticketing.Backend interface.
// Every method succeeds silently, making it safe to use as a default
// when no real ticketing provider is configured.
//
// When TaskFile is set, PollReadyTickets reads tasks from the specified
// YAML file instead of returning an empty slice.
type Backend struct {
	logger   *slog.Logger
	taskFile string

	mu        sync.Mutex
	processed map[string]bool // tracks ticket IDs already marked in-progress
}

// New returns a new no-op ticketing backend.
func New() *Backend {
	return &Backend{
		logger:    slog.Default(),
		processed: make(map[string]bool),
	}
}

// NewWithTaskFile returns a noop backend that reads tasks from a local YAML
// file. Each poll reads the file and returns any tasks not yet marked
// in-progress, enabling a simple file-watcher workflow for local development.
func NewWithTaskFile(logger *slog.Logger, taskFile string) *Backend {
	return &Backend{
		logger:    logger,
		taskFile:  taskFile,
		processed: make(map[string]bool),
	}
}

// PollReadyTickets returns tickets read from the configured task file. If no
// task file is set, it returns an empty slice (standard noop behaviour).
// Tickets that have already been marked in-progress are excluded.
func (b *Backend) PollReadyTickets(ctx context.Context) ([]ticketing.Ticket, error) {
	if b.taskFile == "" {
		b.logger.DebugContext(ctx, "noop ticketing: poll ready tickets (returning empty)")
		return nil, nil
	}

	tickets, err := b.readTaskFile()
	if err != nil {
		return nil, fmt.Errorf("reading task file: %w", err)
	}

	// Filter out already-processed tickets.
	b.mu.Lock()
	defer b.mu.Unlock()

	var ready []ticketing.Ticket
	for _, t := range tickets {
		if !b.processed[t.ID] {
			ready = append(ready, t)
		}
	}

	if len(ready) > 0 {
		b.logger.InfoContext(ctx, "noop ticketing: polled tasks from file",
			"file", b.taskFile,
			"total", len(tickets),
			"ready", len(ready),
		)
	}

	return ready, nil
}

// readTaskFile reads and parses the YAML task file into ticketing.Ticket slice.
func (b *Backend) readTaskFile() ([]ticketing.Ticket, error) {
	data, err := os.ReadFile(b.taskFile)
	if err != nil {
		return nil, fmt.Errorf("reading file %q: %w", b.taskFile, err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	var tasks []FileTask
	if err := yaml.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("parsing task file %q: %w", b.taskFile, err)
	}

	tickets := make([]ticketing.Ticket, 0, len(tasks))
	for _, ft := range tasks {
		tickets = append(tickets, ticketing.Ticket{
			ID:          ft.ID,
			Title:       ft.Title,
			Description: ft.Description,
			RepoURL:     ft.RepoURL,
			Labels:      ft.Labels,
		})
	}

	return tickets, nil
}

// MarkInProgress silently accepts the transition. When a task file is
// configured, the ticket ID is recorded so it will not be returned by
// future PollReadyTickets calls.
func (b *Backend) MarkInProgress(ctx context.Context, ticketID string) error {
	b.logger.DebugContext(ctx, "noop ticketing: mark in progress", "ticket_id", ticketID)
	b.mu.Lock()
	b.processed[ticketID] = true
	b.mu.Unlock()
	return nil
}

// MarkComplete silently accepts the transition.
func (b *Backend) MarkComplete(ctx context.Context, ticketID string, result engine.TaskResult) error {
	b.logger.DebugContext(ctx, "noop ticketing: mark complete", "ticket_id", ticketID)
	return nil
}

// MarkFailed silently accepts the transition.
func (b *Backend) MarkFailed(ctx context.Context, ticketID string, reason string) error {
	b.logger.DebugContext(ctx, "noop ticketing: mark failed", "ticket_id", ticketID, "reason", reason)
	return nil
}

// AddComment silently discards the comment.
func (b *Backend) AddComment(ctx context.Context, ticketID string, comment string) error {
	b.logger.DebugContext(ctx, "noop ticketing: add comment", "ticket_id", ticketID)
	return nil
}

// Name returns the backend identifier.
func (b *Backend) Name() string {
	return "noop"
}

// InterfaceVersion returns the ticketing interface version this backend implements.
func (b *Backend) InterfaceVersion() int {
	return ticketing.InterfaceVersion
}
