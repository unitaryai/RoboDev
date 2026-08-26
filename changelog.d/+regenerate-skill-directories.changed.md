**Skill and sub-agent directories are cleared before being regenerated at
pod start.** They are rebuilt from the `CLAUDE_SKILL_*` and
`CLAUDE_SUBAGENT_PATH_*` environment variables on every start, but were
previously written over whatever was already there. On a persisted config
directory that meant a skill removed from the controller's configuration
survived as a stale file into later runs of the same TaskRun. The
environment variables are now the single source of truth.
