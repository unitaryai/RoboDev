**Stream-reader gating now uses an engine capability instead of an engine
name string**: introduced `engine.StreamEmitter`
(`pkg/engine/capabilities.go`), a capability interface engines implement
when their agent pods write a machine-readable event stream to stdout.
`ClaudeCodeEngine` implements it. The reconciler's eight stream-reader
gates (previously `engineName == "claude-code"` checks scattered across
`internal/controller/controller.go` and `internal/controller/incident.go`)
now go through one helper, `Reconciler.engineEmitsStream`, which looks up
the dispatched engine and asserts the interface. Behaviour for claude-code
is byte-identical, pinned by new golden tests in `pkg/engine/claudecode`
covering `BuildExecutionSpec` and `BuildPrompt` for five task shapes.
Other `"claude-code"` string comparisons (auth config merge, engine
defaults, continuation config) are unrelated to stream gating and are left
for follow-up PRs.
