package engine

// StreamFormat identifies the stdout wire format an engine's agent pods emit.
type StreamFormat string

const (
	// StreamFormatOsmia is the native NDJSON envelope format parsed by
	// internal/agentstream (Claude Code stream-json and the e2e fake agent).
	StreamFormatOsmia StreamFormat = "osmia"
)

// StreamEmitter is implemented by engines whose pods write a machine-readable
// event stream to stdout that the controller can follow and parse.
type StreamEmitter interface {
	// StreamFormat returns the wire format of the event stream this engine's
	// pods write to stdout.
	StreamFormat() StreamFormat
}

// CredentialHints lets an engine declare the environment variable its CLI
// reads its API key from, and the well-known secret key names to probe when
// the operator has not set an explicit api_key_key.
type CredentialHints interface {
	// APIKeyEnvName returns the environment variable the engine's CLI reads
	// its API key from (e.g. "ANTHROPIC_API_KEY").
	APIKeyEnvName() string
	// APIKeyKeyCandidates returns the well-known secret key names to probe,
	// in priority order, when no explicit api_key_key is configured.
	APIKeyKeyCandidates() []string
}
