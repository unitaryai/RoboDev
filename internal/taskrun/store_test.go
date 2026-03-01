package taskrun

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_SaveAndGet(t *testing.T) {
	tests := []struct {
		name    string
		tr      *TaskRun
		wantErr bool
	}{
		{
			name: "save and retrieve a task run",
			tr:   New("tr-1", "key-1", "TICKET-1", "claude-code"),
		},
		{
			name: "overwrite existing task run",
			tr:   New("tr-1", "key-1", "TICKET-1", "codex"),
		},
		{
			name:    "save nil returns error",
			tr:      nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			err := store.Save(ctx, tt.tr)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			got, err := store.Get(ctx, tt.tr.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.tr.ID, got.ID)
			assert.Equal(t, tt.tr.Engine, got.Engine)
		})
	}
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMemoryStore_List(t *testing.T) {
	tests := []struct {
		name  string
		setup []*TaskRun
		want  int
	}{
		{
			name:  "empty store returns empty list",
			setup: nil,
			want:  0,
		},
		{
			name: "returns all stored task runs",
			setup: []*TaskRun{
				New("tr-1", "key-1", "TICKET-1", "claude-code"),
				New("tr-2", "key-2", "TICKET-2", "codex"),
				New("tr-3", "key-3", "TICKET-3", "aider"),
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			for _, tr := range tt.setup {
				require.NoError(t, store.Save(ctx, tr))
			}

			got, err := store.List(ctx)
			require.NoError(t, err)
			assert.Len(t, got, tt.want)
		})
	}
}

func TestMemoryStore_ListByTicketID(t *testing.T) {
	tests := []struct {
		name     string
		setup    []*TaskRun
		ticketID string
		want     int
	}{
		{
			name: "filters by ticket ID",
			setup: []*TaskRun{
				New("tr-1", "key-1", "TICKET-1", "claude-code"),
				New("tr-2", "key-2", "TICKET-1", "codex"),
				New("tr-3", "key-3", "TICKET-2", "aider"),
			},
			ticketID: "TICKET-1",
			want:     2,
		},
		{
			name: "returns empty for unmatched ticket ID",
			setup: []*TaskRun{
				New("tr-1", "key-1", "TICKET-1", "claude-code"),
			},
			ticketID: "TICKET-999",
			want:     0,
		},
		{
			name:     "empty store returns empty list",
			setup:    nil,
			ticketID: "TICKET-1",
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			for _, tr := range tt.setup {
				require.NoError(t, store.Save(ctx, tr))
			}

			got, err := store.ListByTicketID(ctx, tt.ticketID)
			require.NoError(t, err)
			assert.Len(t, got, tt.want)

			for _, tr := range got {
				assert.Equal(t, tt.ticketID, tr.TicketID)
			}
		})
	}
}
