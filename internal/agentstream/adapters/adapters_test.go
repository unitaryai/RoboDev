package adapters

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unitaryai/osmia/pkg/engine"
)

func TestFor(t *testing.T) {
	tests := []struct {
		name       string
		format     engine.StreamFormat
		wantExists bool
	}{
		{
			name:       "osmia format is registered",
			format:     engine.StreamFormatOsmia,
			wantExists: true,
		},
		{
			name:       "unknown format is not registered",
			format:     engine.StreamFormat("codex-jsonl"),
			wantExists: false,
		},
		{
			name:       "empty format is not registered",
			format:     engine.StreamFormat(""),
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator, ok := For(tt.format)
			assert.Equal(t, tt.wantExists, ok)
			if tt.wantExists {
				require.NotNil(t, translator)
			} else {
				assert.Nil(t, translator)
			}
		})
	}
}
