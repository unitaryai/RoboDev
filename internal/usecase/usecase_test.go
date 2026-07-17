package usecase

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGates_ZeroValueIsGateFree(t *testing.T) {
	var g Gates

	assert.False(t, g.ApprovalGates)
	assert.False(t, g.CostEstimation)
	assert.False(t, g.EngineSelector)
	assert.False(t, g.GuardRails)
	assert.False(t, g.MarkInProgress)
	assert.False(t, g.MemoryQuery)
	assert.False(t, g.NotifyStart)
	assert.False(t, g.RepoURLRequired)
	assert.False(t, g.Tournament)
}

func TestExecutionMode_Constants(t *testing.T) {
	tests := []struct {
		mode ExecutionMode
		want string
	}{
		{mode: ModeClonePushMR, want: "clone_push_mr"},
		{mode: ModeReadOnly, want: "read_only"},
		{mode: ModeAPIRead, want: "api_read"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.mode))
		})
	}
}
