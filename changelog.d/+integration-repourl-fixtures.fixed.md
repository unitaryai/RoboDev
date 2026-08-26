**Repo-URL fixtures in `tests/integration`**: nine tests predated the
hard gate in `ProcessTicket`/`resolveRepoURL` (`internal/controller`)
that requires a resolvable repository URL — either on `ticket.RepoURL`
or extractable from the description — before a ticket is processed, and
now fail with "no repository URL found in ticket description and no
interactive channel configured to ask" unless a URL is present. Added
`RepoURL: "https://github.com/org/repo"` to the affected ticket
fixtures in `guardrails_test.go`, `engine_fallback_test.go`,
`prm_controller_test.go`, and `memory_controller_test.go`, matching the
convention already used by the passing tests in the same files.
`TestTaskRunInvalidTransitions` (`taskrun_lifecycle_test.go`) failed for
an unrelated reason surfaced by running the full suite: it asserted
`NeedsHuman→Failed` was an invalid transition, but that transition has
been legitimately allowed since `internal/taskrun`'s state machine was
extended (so the repo-URL poller can fail a TaskRun directly from
`NeedsHuman`); the stale case was removed from the test's invalid-
transition table.
