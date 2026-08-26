**Translator seam in `internal/agentstream`**: a new `Translator` interface
converts one raw stdout line from an agent pod into zero or more stream
events, so a future engine's native output format (e.g. Codex JSONL) can
be normalised into Osmia's NDJSON envelopes without teaching the core
package about engine-specific formats. `internal/agentstream/adapters`
registers a `Translator` per `engine.StreamFormat`; only
`StreamFormatOsmia` is registered so far, mapping to a new
`passthroughTranslator` that wraps the existing `ParseEvent` unchanged.
`Reader` gains a `WithTranslator` option (default passthrough), and the
controller's `startStreamReader` resolves the dispatched engine's
translator via `adapters.For` and passes it through. `engineEmitsStream`
is refactored onto the same lookup (via `streamTranslatorFor`) so the
stream-reader gate and the reader construction cannot disagree. This PR
is passthrough-only: behaviour is byte-identical to before, and no Codex
adapter is added yet.
