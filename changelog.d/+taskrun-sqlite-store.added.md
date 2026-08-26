**SQLite `TaskRunStore` backend**: `internal/taskrun/sqlite.go` adds a
`SQLiteStore` implementing the existing `TaskRunStore` interface,
following the same pattern as the other in-house SQLite-backed stores
(`modernc.org/sqlite`, WAL mode, JSON-blob content column with promoted
query columns). Nothing in the controller constructs this store yet;
`MemoryStore` remains the only store actually wired up, so this change
has no effect on runtime behaviour. See
`docs/adr/0001-taskrun-store-sqlite-not-crd.md` for the decision to use
SQLite rather than a `TaskRun` CRD.
