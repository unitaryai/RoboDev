package agentstream

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCapturingLogger returns a *slog.Logger whose output is written to buf.
// The text handler is used so that key=value pairs are easy to assert against.
func newCapturingLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	return slog.New(slog.NewTextHandler(buf, opts))
}

func TestNewLoggingEventProcessor_ToolCall(t *testing.T) {
	tests := []struct {
		name       string
		event      *StreamEvent
		wantSubstr []string
		notSubstr  []string
	}{
		{
			name: "tool call with short args",
			event: &StreamEvent{
				Type: EventToolCall,
				Parsed: &ToolCallEvent{
					Tool: "Bash",
					Args: json.RawMessage(`{"command":"ls -la"}`),
				},
			},
			// The text handler quotes the value, so braces are escaped.
			// We assert on the tool name and key substring rather than the
			// raw JSON literal, since slog.NewTextHandler escapes the braces.
			wantSubstr: []string{
				"agent tool call",
				`tool=Bash`,
				`command`,
				`ls -la`,
			},
		},
		{
			name: "tool call with args exceeding 80 chars is truncated",
			event: &StreamEvent{
				Type: EventToolCall,
				Parsed: &ToolCallEvent{
					Tool: "Bash",
					// 90-character string so we can verify truncation to 80.
					Args: json.RawMessage(`{"command":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
				},
			},
			wantSubstr: []string{"agent tool call", "tool=Bash"},
			// The full 90-char value must not appear in the log output.
			notSubstr: []string{`"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`},
		},
		{
			name: "tool call with empty args",
			event: &StreamEvent{
				Type:   EventToolCall,
				Parsed: &ToolCallEvent{Tool: "Read"},
			},
			wantSubstr: []string{"agent tool call", "tool=Read", "input="},
		},
		{
			name: "tool call with nil Parsed is a no-op",
			event: &StreamEvent{
				Type:   EventToolCall,
				Parsed: nil,
			},
			// No output expected — processor returns early.
			wantSubstr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newCapturingLogger(&buf, slog.LevelDebug)
			proc := NewLoggingEventProcessor(logger)

			proc(context.Background(), tt.event)

			output := buf.String()
			for _, want := range tt.wantSubstr {
				assert.Contains(t, output, want, "expected substring %q not found in log output", want)
			}
			for _, notWant := range tt.notSubstr {
				assert.NotContains(t, output, notWant, "unexpected substring %q found in log output", notWant)
			}
		})
	}
}

func TestNewLoggingEventProcessor_ContentDelta(t *testing.T) {
	tests := []struct {
		name      string
		event     *StreamEvent
		wantLog   bool
		wantRole  string
		notSubstr []string
	}{
		{
			name: "content event logs role but not content text",
			event: &StreamEvent{
				Type: EventContentDelta,
				Parsed: &ContentDeltaEvent{
					Content: "SECRET_API_KEY=hunter2",
					Role:    "assistant",
				},
			},
			wantLog:  true,
			wantRole: "assistant",
			// The actual content text must never appear in logs.
			notSubstr: []string{"SECRET_API_KEY", "hunter2"},
		},
		{
			name: "content event with user role",
			event: &StreamEvent{
				Type: EventContentDelta,
				Parsed: &ContentDeltaEvent{
					Content: "some user message",
					Role:    "user",
				},
			},
			wantLog:   true,
			wantRole:  "user",
			notSubstr: []string{"some user message"},
		},
		{
			name: "content event with nil Parsed is a no-op",
			event: &StreamEvent{
				Type:   EventContentDelta,
				Parsed: nil,
			},
			wantLog: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newCapturingLogger(&buf, slog.LevelDebug)
			proc := NewLoggingEventProcessor(logger)

			proc(context.Background(), tt.event)

			output := buf.String()

			if tt.wantLog {
				assert.Contains(t, output, "agent content")
				assert.Contains(t, output, "role="+tt.wantRole)
			} else {
				assert.Empty(t, output)
			}

			for _, notWant := range tt.notSubstr {
				assert.NotContains(t, output, notWant,
					"sensitive content %q must not appear in log output", notWant)
			}
		})
	}
}

func TestNewLoggingEventProcessor_Result(t *testing.T) {
	tests := []struct {
		name       string
		event      *StreamEvent
		wantSubstr []string
		notSubstr  []string
	}{
		{
			name: "successful result with MR URL",
			event: &StreamEvent{
				Type: EventResult,
				Parsed: &ResultEvent{
					Success:         true,
					Summary:         "Fixed the bug",
					MergeRequestURL: "https://github.com/org/repo/pull/42",
				},
			},
			wantSubstr: []string{
				"agent result",
				"success=true",
				"summary=",
				"Fixed the bug",
				"mr_url=https://github.com/org/repo/pull/42",
			},
		},
		{
			name: "failed result without MR URL omits mr_url key",
			event: &StreamEvent{
				Type: EventResult,
				Parsed: &ResultEvent{
					Success: false,
					Summary: "Task failed",
				},
			},
			wantSubstr: []string{
				"agent result",
				"success=false",
				"summary=",
			},
			notSubstr: []string{"mr_url"},
		},
		{
			name: "result with nil Parsed is a no-op",
			event: &StreamEvent{
				Type:   EventResult,
				Parsed: nil,
			},
			wantSubstr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newCapturingLogger(&buf, slog.LevelDebug)
			proc := NewLoggingEventProcessor(logger)

			proc(context.Background(), tt.event)

			output := buf.String()
			for _, want := range tt.wantSubstr {
				assert.Contains(t, output, want)
			}
			for _, notWant := range tt.notSubstr {
				assert.NotContains(t, output, notWant)
			}
		})
	}
}

func TestNewLoggingEventProcessor_Cost(t *testing.T) {
	tests := []struct {
		name       string
		event      *StreamEvent
		wantSubstr []string
	}{
		{
			name: "cost event logs token counts and USD cost",
			event: &StreamEvent{
				Type: EventCost,
				Parsed: &CostEvent{
					InputTokens:  1500,
					OutputTokens: 300,
					CostUSD:      0.012,
				},
			},
			wantSubstr: []string{
				"agent cost",
				"input_tokens=1500",
				"output_tokens=300",
				"cost_usd=",
			},
		},
		{
			name: "cost event with nil Parsed is a no-op",
			event: &StreamEvent{
				Type:   EventCost,
				Parsed: nil,
			},
			wantSubstr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newCapturingLogger(&buf, slog.LevelDebug)
			proc := NewLoggingEventProcessor(logger)

			proc(context.Background(), tt.event)

			output := buf.String()
			for _, want := range tt.wantSubstr {
				assert.Contains(t, output, want)
			}
		})
	}
}

func TestNewLoggingEventProcessor_SystemAndUnknown(t *testing.T) {
	tests := []struct {
		name       string
		event      *StreamEvent
		wantSubstr []string
	}{
		{
			name: "system event",
			event: &StreamEvent{
				Type:   EventSystem,
				Parsed: nil,
			},
			wantSubstr: []string{
				"agent system event",
				"type=system",
			},
		},
		{
			name: "unknown event type",
			event: &StreamEvent{
				Type:   EventType("heartbeat"),
				Parsed: nil,
			},
			wantSubstr: []string{
				"agent system event",
				"type=heartbeat",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newCapturingLogger(&buf, slog.LevelDebug)
			proc := NewLoggingEventProcessor(logger)

			proc(context.Background(), tt.event)

			output := buf.String()
			for _, want := range tt.wantSubstr {
				assert.Contains(t, output, want)
			}
		})
	}
}

func TestNewLoggingEventProcessor_ContentNotLoggedAtInfoLevel(t *testing.T) {
	// Even when the logger is configured at Info level (the common production
	// default), content events must produce no output because the handler
	// defaults to Info and Debug lines are suppressed. This is an additional
	// guard against accidental content leakage.
	var buf bytes.Buffer
	// Info-level logger — Debug lines are suppressed.
	logger := newCapturingLogger(&buf, slog.LevelInfo)
	proc := NewLoggingEventProcessor(logger)

	proc(context.Background(), &StreamEvent{
		Type: EventContentDelta,
		Parsed: &ContentDeltaEvent{
			Content: "this must never appear",
			Role:    "assistant",
		},
	})

	require.Empty(t, buf.String(), "content events must produce no output at Info log level")
}

func TestNewLoggingEventProcessor_ToolCallArgs80CharBoundary(t *testing.T) {
	// Verify the exact boundary: a string of exactly 80 characters is not
	// truncated, whilst 81 characters is. We use a plain alphanumeric string
	// so that the text handler does not escape any characters, making the
	// substring assertions straightforward.
	//
	// "aaaaa...a" — 80 'a' characters.
	exactly80 := string(bytes.Repeat([]byte("a"), 80))
	require.Len(t, exactly80, 80)

	over80 := exactly80 + "Z"
	require.Len(t, over80, 81)

	for _, tc := range []struct {
		name       string
		args       string
		expectFull bool
	}{
		{"exactly 80 chars not truncated", exactly80, true},
		{"81 chars truncated", over80, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newCapturingLogger(&buf, slog.LevelDebug)
			proc := NewLoggingEventProcessor(logger)

			proc(context.Background(), &StreamEvent{
				Type: EventToolCall,
				Parsed: &ToolCallEvent{
					Tool: "Bash",
					Args: json.RawMessage(tc.args),
				},
			})

			output := buf.String()
			if tc.expectFull {
				assert.Contains(t, output, tc.args)
			} else {
				// The full 81-char string must not appear.
				assert.NotContains(t, output, tc.args)
				// But the first 80 characters must still be present.
				assert.Contains(t, output, tc.args[:80])
			}
		})
	}
}
