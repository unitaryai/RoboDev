package usecase

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	tests := []struct {
		name        string
		def         *Definition
		registerErr string
	}{
		{
			name: "registers and retrieves a definition",
			def:  &Definition{Name: NameTicketing, ExecutionMode: ModeClonePushMR},
		},
		{
			name: "registers and retrieves a second definition",
			def:  &Definition{Name: NameIncidentTriage, ExecutionMode: ModeAPIRead},
		},
		{
			name:        "rejects a nil definition",
			def:         nil,
			registerErr: "nil definition",
		},
		{
			name:        "rejects a definition with an empty name",
			def:         &Definition{ExecutionMode: ModeReadOnly},
			registerErr: "empty name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewRegistry()
			err := reg.Register(tt.def)

			if tt.registerErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.registerErr)
				return
			}
			require.NoError(t, err)

			got, ok := reg.Get(tt.def.Name)
			require.True(t, ok)
			assert.Equal(t, tt.def, got)
		})
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	reg := NewRegistry()

	_, ok := reg.Get("does-not-exist")
	assert.False(t, ok)
}

func TestRegistry_RegisterReplacesExisting(t *testing.T) {
	reg := NewRegistry()

	require.NoError(t, reg.Register(&Definition{Name: NameTicketing, ExecutionMode: ModeClonePushMR}))
	require.NoError(t, reg.Register(&Definition{Name: NameTicketing, ExecutionMode: ModeReadOnly}))

	got, ok := reg.Get(NameTicketing)
	require.True(t, ok)
	assert.Equal(t, ModeReadOnly, got.ExecutionMode)
}
