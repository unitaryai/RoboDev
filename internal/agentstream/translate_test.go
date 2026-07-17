package agentstream

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPassthroughTranslator_MatchesParseEvent asserts that the passthrough
// translator produces exactly the same outcome as calling ParseEvent
// directly, for both valid and malformed lines. This is the seam's
// passthrough guarantee: existing behaviour must be byte-identical.
func TestPassthroughTranslator_MatchesParseEvent(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{
			name: "valid tool_use envelope",
			line: `{"type":"tool_use","tool":"Bash","args":{"command":"ls"},"timestamp":"2026-01-01T00:00:00Z"}`,
		},
		{
			name: "valid content envelope",
			line: `{"type":"content","content":"hello","role":"assistant","timestamp":"2026-01-01T00:00:00Z"}`,
		},
		{
			name: "valid result envelope",
			line: `{"type":"result","success":true,"summary":"done","timestamp":"2026-01-01T00:00:00Z"}`,
		},
		{
			name: "unknown event type",
			line: `{"type":"heartbeat","timestamp":"2026-01-01T00:00:00Z"}`,
		},
		{
			name: "malformed json",
			line: `this is not json`,
		},
		{
			name: "missing type field",
			line: `{"timestamp":"2026-01-01T00:00:00Z"}`,
		},
		{
			name: "invalid tool_use payload",
			line: `{"type":"tool_use","args":123}`,
		},
	}

	translator := NewPassthroughTranslator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantEvent, wantErr := ParseEvent([]byte(tt.line))

			gotEvents, gotErr := translator.Translate([]byte(tt.line))

			if wantErr != nil {
				require.Error(t, gotErr)
				assert.EqualError(t, gotErr, wantErr.Error())
				assert.Nil(t, gotEvents)
				return
			}

			require.NoError(t, gotErr)
			require.Len(t, gotEvents, 1)
			assert.Equal(t, wantEvent, gotEvents[0])
		})
	}
}

// TestPassthroughTranslator_EmptyLine documents that Translate, like
// ParseEvent, treats an empty line as an error rather than a skip. The
// Reader loop is what filters empty lines before ever calling Translate.
func TestPassthroughTranslator_EmptyLine(t *testing.T) {
	translator := NewPassthroughTranslator()

	events, err := translator.Translate([]byte(""))

	assert.Error(t, err)
	assert.Nil(t, events)
}
