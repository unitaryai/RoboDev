**SQLite `TaskRunStore` backend**: `internal/taskrun/sqlite.go` adds a
`SQLiteStore` implementing the existing `TaskRunStore` interface,
following the same pattern as the other in-house SQLite-backed stores
(`modernc.org/sqlite`, WAL mode, JSON-blob content column with promoted
query columns). `MemoryStore` remains the default, so no existing
deployment changes behaviour; selecting `SQLiteStore` is done through
`taskrun_store.backend`, which has its own entry in this release. See
`docs/adr/0001-taskrun-store-sqlite-not-crd.md` for the decision to use
SQLite rather than a `TaskRun` CRD.
