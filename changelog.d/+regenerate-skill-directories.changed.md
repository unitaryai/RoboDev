**Skill and sub-agent directories are cleared on every pod start.** They are
rebuilt from the `CLAUDE_SKILL_*` and `CLAUDE_SUBAGENT_PATH_*` environment
variables, but were previously written over whatever was already there. On a
persisted configuration directory that meant a skill removed from the
controller's configuration survived as a stale file into later runs. The
clear runs unconditionally rather than only when those variables are present,
so removing the *last* configured skill also takes effect; guarding it would
have skipped exactly the case that needs it. `setup-claude.sh` additionally
canonicalises the configuration directory before deriving those paths and
refuses to start if it does not resolve to a nested absolute path, so a
malformed value such as `/tmp/..` cannot point the clear at `/skills`, where
the ConfigMap volumes are mounted.
