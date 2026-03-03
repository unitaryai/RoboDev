package agentstream

import (
	"context"
	"log/slog"
)

// inputSnippetLen is the maximum number of characters from a tool call's args
// JSON that are included in the log line. Truncating avoids flooding logs with
// large payloads whilst still giving enough context for debugging.
const inputSnippetLen = 80

// NewLoggingEventProcessor returns a StreamEventProcessor that logs each event
// as a structured, human-readable slog line. Content events are elided to avoid
// logging raw LLM output which may contain sensitive information.
func NewLoggingEventProcessor(logger *slog.Logger) StreamEventProcessor {
	return func(ctx context.Context, event *StreamEvent) {
		switch event.Type {
		case EventToolCall:
			tc, ok := event.Parsed.(*ToolCallEvent)
			if !ok || tc == nil {
				return
			}

			input := ""
			if len(tc.Args) > 0 {
				s := string(tc.Args)
				if len(s) > inputSnippetLen {
					s = s[:inputSnippetLen]
				}
				input = s
			}

			logger.InfoContext(ctx, "agent tool call",
				slog.String("tool", tc.Tool),
				slog.String("input", input),
			)

		case EventContentDelta:
			cd, ok := event.Parsed.(*ContentDeltaEvent)
			if !ok || cd == nil {
				return
			}
			// Content text is deliberately not logged — it may contain
			// sensitive information or generate excessive log volume.
			logger.DebugContext(ctx, "agent content",
				slog.String("role", cd.Role),
			)

		case EventResult:
			re, ok := event.Parsed.(*ResultEvent)
			if !ok || re == nil {
				return
			}

			attrs := []any{
				slog.Bool("success", re.Success),
				slog.String("summary", re.Summary),
			}
			if re.MergeRequestURL != "" {
				attrs = append(attrs, slog.String("mr_url", re.MergeRequestURL))
			}

			logger.InfoContext(ctx, "agent result", attrs...)

		case EventCost:
			ce, ok := event.Parsed.(*CostEvent)
			if !ok || ce == nil {
				return
			}

			logger.InfoContext(ctx, "agent cost",
				slog.Int("input_tokens", ce.InputTokens),
				slog.Int("output_tokens", ce.OutputTokens),
				slog.Float64("cost_usd", ce.CostUSD),
			)

		default:
			// Covers EventSystem and any unknown event types.
			logger.DebugContext(ctx, "agent system event",
				slog.String("type", string(event.Type)),
			)
		}
	}
}
