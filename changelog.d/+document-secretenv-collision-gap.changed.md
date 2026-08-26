**Documented the limit of the task-secret collision check.** The check
compares a task's declared environment variable names against the engine's
execution spec, but an engine can attach a whole Kubernetes Secret with
`envFrom`, which injects every key it contains. Osmia knows that Secret's
name and not its contents, so a task naming one of those keys would shadow it
undetected, since Kubernetes gives an explicit variable precedence over one
from `envFrom`. Closing this would mean reading every referenced Secret on
every launch; `blocked_env_patterns` is the intended control and is stronger,
because it does not depend on knowing what any Secret holds. Written up in
`docs/getting-started/configuration.md` and at the check itself.
