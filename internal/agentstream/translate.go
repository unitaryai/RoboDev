package agentstream

// Translator converts one raw stdout line from an agent pod into zero or
// more stream events. Implementations exist per engine stream format; the
// passthrough translator handles Osmia's native NDJSON envelope format.
//
// Translator is the seam future engines (e.g. Codex, which emits its own
// JSONL format) plug into: each engine's stdout format gets its own
// Translator implementation, registered against its engine.StreamFormat in
// internal/agentstream/adapters, so the reader and forwarder pipeline stay
// unaware of engine-specific wire formats.
type Translator interface {
	// Translate returns the events carried by one log line. A (nil, nil)
	// return means the line carries no signal and is skipped.
	Translate(line []byte) ([]*StreamEvent, error)
}

// passthroughTranslator is the Translator for Osmia's native NDJSON envelope
// format. It delegates directly to ParseEvent, preserving today's behaviour
// exactly: a single line always yields at most one event.
type passthroughTranslator struct{}

// NewPassthroughTranslator returns the Translator for Osmia's native NDJSON
// envelope format (Claude Code stream-json and the e2e fake agent). It wraps
// ParseEvent without changing its parsing or error semantics.
func NewPassthroughTranslator() Translator {
	return passthroughTranslator{}
}

// Translate implements Translator by parsing line as a single Osmia NDJSON
// envelope via ParseEvent.
func (passthroughTranslator) Translate(line []byte) ([]*StreamEvent, error) {
	ev, err := ParseEvent(line)
	if err != nil {
		return nil, err
	}
	return []*StreamEvent{ev}, nil
}
