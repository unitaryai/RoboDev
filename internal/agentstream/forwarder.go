package agentstream

import (
	"context"
	"log/slog"

	"github.com/unitaryai/robodev/pkg/plugin/notifications"
)

// Forwarder consumes parsed StreamEvents and distributes them to the
// watchdog channel and notification backends. It is the glue between the
// raw agent stream and the rest of the controller.
type Forwarder struct {
	logger     *slog.Logger
	watchdogCh chan<- *StreamEvent
	notifiers  []notifications.Channel
}

// ForwarderOption configures optional Forwarder behaviour.
type ForwarderOption func(*Forwarder)

// WithWatchdogChannel configures the Forwarder to forward every event to
// the given channel, which is typically consumed by the watchdog.
func WithWatchdogChannel(ch chan<- *StreamEvent) ForwarderOption {
	return func(f *Forwarder) {
		f.watchdogCh = ch
	}
}

// WithNotifiers configures the Forwarder to send selected events to the
// given notification channels.
func WithNotifiers(channels []notifications.Channel) ForwarderOption {
	return func(f *Forwarder) {
		f.notifiers = channels
	}
}

// NewForwarder creates a Forwarder with the given logger and options.
func NewForwarder(logger *slog.Logger, opts ...ForwarderOption) *Forwarder {
	f := &Forwarder{
		logger: logger,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Forward reads events from eventCh until the channel is closed or the
// context is cancelled. Each event is forwarded to the watchdog channel
// (if configured) and selected events trigger notification side-effects.
func (f *Forwarder) Forward(ctx context.Context, eventCh <-chan *StreamEvent) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-eventCh:
			if !ok {
				return nil
			}
			f.handle(ctx, ev)
		}
	}
}

// handle dispatches a single event to the watchdog and any notification
// channels that should receive it.
func (f *Forwarder) handle(ctx context.Context, ev *StreamEvent) {
	// Always forward to the watchdog if configured.
	if f.watchdogCh != nil {
		select {
		case f.watchdogCh <- ev:
		case <-ctx.Done():
			return
		}
	}

	switch ev.Type {
	case EventResult:
		f.handleResult(ctx, ev)
	case EventCost:
		f.handleCost(ev)
	case EventToolCall:
		f.handleToolCall(ev)
	}
}

// handleResult logs the completion summary from a ResultEvent.
func (f *Forwarder) handleResult(_ context.Context, ev *StreamEvent) {
	re, ok := ev.Parsed.(*ResultEvent)
	if !ok || re == nil {
		return
	}

	f.logger.Info("agent run completed",
		"success", re.Success,
		"summary", re.Summary,
		"tests_passed", re.TestsPassed,
		"tests_failed", re.TestsFailed,
	)
}

// handleCost logs token usage and cost data from a CostEvent.
func (f *Forwarder) handleCost(ev *StreamEvent) {
	ce, ok := ev.Parsed.(*CostEvent)
	if !ok || ce == nil {
		return
	}

	f.logger.Info("agent cost update",
		"input_tokens", ce.InputTokens,
		"output_tokens", ce.OutputTokens,
		"cost_usd", ce.CostUSD,
	)
}

// handleToolCall logs tool invocations for observability.
func (f *Forwarder) handleToolCall(ev *StreamEvent) {
	tc, ok := ev.Parsed.(*ToolCallEvent)
	if !ok || tc == nil {
		return
	}

	f.logger.Debug("agent tool call",
		"tool", tc.Tool,
	)
}
