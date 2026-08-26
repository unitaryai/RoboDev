**Custom skills and sub-agents were invisible to the agent whenever session
persistence was enabled.** A previous fix moved them to `${HOME}/.claude/`
unconditionally, on the stated grounds that Claude Code's discovery ignored
`CLAUDE_CONFIG_DIR`. That is not true: when `CLAUDE_CONFIG_DIR` is set,
Claude Code reads user-scoped skills and sub-agents from under it and does
not fall back to `${HOME}`. Since `CLAUDE_CONFIG_DIR` is set only by the
session-persistence backends, every such deployment delivered its skills to
a directory the agent never read. The failure was silent: the skill simply
never appeared, and the first `/<name>` invocation returned "Unknown skill"
before the agent burned turns recovering by hand. Verified by direct repro
against claude-code 2.0.28, 2.1.145 and 2.1.160, and pinned by
`hack/test-skill-placement.sh`. Deployments with session persistence
disabled were unaffected, which is why the default path never showed it.
