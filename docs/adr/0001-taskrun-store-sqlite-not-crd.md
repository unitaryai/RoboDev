# ADR 0001: Persist TaskRun state in SQLite, not a Kubernetes CRD

## Status

Accepted

## Context

`TaskRun` is the controller's record of a single execution of a task,
tracking its lifecycle from creation through completion (state, retry
count, engine attempts, heartbeat, diagnosis history, and so on). Today
`TaskRun` lives only in `MemoryStore`, an in-memory map guarded by a mutex.
When the controller pod restarts, that map is gone. Every in-flight
TaskRun is forgotten, along with the idempotency key that would otherwise
stop the controller from launching a duplicate Job for the same ticket.
Orphaned-run recovery after a controller restart is the top gap on the
project roadmap, and it cannot be fixed without durable storage underneath
`TaskRun`.

`oss-plan.md` (section 13, open question 7) left the underlying storage
model undecided: should `TaskRun` be a proper Kubernetes CRD, visible via
`kubectl` and surviving controller restarts, or an in-memory structure
backed by some other persistence mechanism? `CLAUDE.md` explicitly forbids
adding Kubernetes CRD types "until explicitly decided", so this question
had to be settled before any durable-storage work could proceed.

The controller is a hand-rolled reconciler built directly on `client-go`
and `hashicorp/go-plugin`; it does not use `controller-runtime`, and it has
no CRD scaffolding, no generated clientset, and no informer machinery.
Introducing a `TaskRun` CRD would mean either adopting controller-runtime
wholesale (a large architectural change affecting the whole controller) or
hand-rolling CRD watch/list/informer logic from scratch. Either path is a
much bigger lift than the durability problem actually requires.

The codebase already has an established, working pattern for durable
local storage: four other components (`internal/memory`,
`internal/routing`, `internal/estimator`, and now `internal/taskrun`) use
`modernc.org/sqlite`, a pure-Go SQLite driver with no CGO dependency. Each
follows the same shape: a `content_json` column holding the full
JSON-marshalled record as the source of truth, a small number of promoted
columns used only as query keys, `PRAGMA journal_mode=WAL` for read
concurrency, and an idempotent `migrate()` step using
`CREATE TABLE IF NOT EXISTS`. Adopting the same pattern for `TaskRun`
keeps the storage architecture consistent across the controller rather
than introducing a second, different durability mechanism.

## Decision

Persist `TaskRun` records in a SQLite database using the same pattern as
the other in-house SQLite-backed stores, rather than as a Kubernetes CRD.

This resolves open question 7 in `oss-plan.md`: `TaskRun` will not become
a CRD. The SQLite-backed `TaskRunStore` (`internal/taskrun/sqlite.go`)
implements the existing `TaskRunStore` interface alongside `MemoryStore`,
so callers that already depend on the interface do not need to change.
Wiring the controller to actually construct and use the SQLite store,
along with any state-recovery logic on startup, is deliberately left to a
later piece of work; this decision only settles the storage model.

## Consequences

- SQLite supports only one writer at a time. Deployed on a
  `ReadWriteOnce` PersistentVolumeClaim, this is fine for a single
  controller replica, which is the only supported topology today.
- Running more than one controller replica against the same SQLite file
  is not supported without further work (for example, leader election so
  only one replica ever holds the write connection). This coupling
  between the storage choice and the single-replica assumption is now
  documented rather than implicit.
- `TaskRun` state is no longer inspectable via `kubectl`, since it is not
  a Kubernetes resource. Operators needing to inspect TaskRun state must
  use controller-exposed APIs or query the SQLite file directly.
- Recovering and rehydrating in-flight TaskRuns from the store after a
  controller restart is not implemented by this decision; it is separate
  follow-up work that builds on top of durable storage now being
  available.

## Revisit Criteria

Reconsider the CRD approach if either of the following becomes a real
requirement:

- **Multi-replica high availability**: running more than one controller
  replica concurrently, which SQLite's single-writer model cannot support
  without additional coordination.
- **`kubectl`-visible task state**: operators needing to list, watch, or
  `kubectl describe` TaskRun state directly through the Kubernetes API,
  which only a CRD can provide natively.

If either becomes a firm requirement, revisit this decision alongside the
question of whether to adopt controller-runtime, since a CRD-backed
`TaskRun` is much more naturally built on top of it than on the current
hand-rolled `client-go` reconciler.
