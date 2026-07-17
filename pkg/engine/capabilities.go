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
