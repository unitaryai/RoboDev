**Extracted the shared task-launch tail from `ProcessTicket` and
`ProcessIncidentEvent`**: added `internal/controller/launch.go` with
`Reconciler.launchTaskRun`, which now runs `prepareSession`,
`BuildExecutionSpec`, `JobBuilder.Build`, the Kubernetes `Job` create,
the transition to `Running`, the `taskRuns`/`engineChains` bookkeeping,
the re-save to the `TaskRunStore`, metrics, and the `engineEmitsStream`
stream-reader gate — previously duplicated near-identically at the end
of both flows. A small companion, `newLaunchTaskRun`, extracts the
shared `TaskRun` construction and initial save that both flows perform
before their own gate/override logic runs. Genuine per-flow variation
(the ticketing engine fallback chain versus incident triage's
single-engine chain, ticketing's `MarkInProgress` call, and the
per-flow completion log message/fields) is captured explicitly via a
new `launchSpec` struct rather than branched on inside the shared
helper. This is a behaviour-frozen refactor: the incident-triage
contract tests and the claude-code golden tests pin the two flows'
externally observable behaviour and pass unmodified.
