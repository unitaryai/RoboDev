**Engine auth merge now uses a capability interface instead of a
hard-coded claude-code lookup**: added `engine.CredentialHints`
(`pkg/engine/capabilities.go`), which an engine implements to declare the
environment variable its CLI reads its API key from and the well-known
secret key names to probe when no explicit `api_key_key` is configured.
`ClaudeCodeEngine` implements it with the same values (`ANTHROPIC_API_KEY`
env, `ANTHROPIC_API_KEY` then `api_key` probe order) it always used.
`EnginesConfig.AuthFor` (`internal/config/config.go`) mirrors the existing
`ImageFor` per-engine-name lookup for the `AuthConfig` block. In
`Reconciler.baseEngineConfig`, the `engineName == "claude-code"` literal is
replaced by a lookup that consults the registered engine's
`CredentialHints` (falling back to claude-code's own defaults when the
engine isn't registered or doesn't implement the interface), so engines
without hints keep relying on `SecretEnv` in `BuildExecutionSpec` exactly
as before. Behaviour for claude-code is byte-identical, pinned by the
`pkg/engine/claudecode` golden tests plus new focused unit tests in
`internal/controller/engine_auth_test.go` covering the probe order,
explicit-key bypass, and the unchanged-for-other-engines case. Codex
gaining its own `CredentialHints` is left for its own modernisation PR.
