// Package adapters registers the agentstream.Translator for each engine
// stream format, keeping internal/agentstream itself free of engine-specific
// imports. Only engine.StreamFormatOsmia is registered today, mapping to the
// passthrough translator; Codex's JSONL format is the next format to land
// here (Stage B PR 1.6).
package adapters

import (
	"github.com/unitaryai/osmia/internal/agentstream"
	"github.com/unitaryai/osmia/pkg/engine"
)

// translators maps each supported engine.StreamFormat to the Translator that
// converts its raw stdout lines into agentstream.StreamEvents.
var translators = map[engine.StreamFormat]agentstream.Translator{
	engine.StreamFormatOsmia: agentstream.NewPassthroughTranslator(),
}

// For returns the Translator registered for format, and whether one is
// registered. Callers should treat an unregistered format as "this engine's
// stream cannot be translated", not start a stream reader for it.
func For(format engine.StreamFormat) (agentstream.Translator, bool) {
	t, ok := translators[format]
	return t, ok
}
