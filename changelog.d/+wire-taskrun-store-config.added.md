**`taskrun_store` config wired into controller startup**: `cmd/osmia/main.go`
now reads `taskrun_store.backend` and constructs the matching
`taskrun.TaskRunStore`. `""`/`"memory"` (the default) is unchanged
behaviour; `"sqlite"` opens `taskrun.NewSQLiteStore` at
`taskrun_store.sqlite.path` (defaulting to `/data/taskruns.db`) and closes
it on shutdown; any other value (including `"postgres"`, which the config
comment has long advertised but nothing implements) fails fast at
startup. `internal/config` validates the backend name and requires an
absolute path when `sqlite.path` is set. This only persists state for
post-mortem inspection today — startup recovery of in-flight TaskRuns is
separate follow-up work. See
`docs/concepts/taskrun-lifecycle.md#durable-taskrun-state` and
`charts/osmia/values.yaml` for the persistence PVC this backend expects.
