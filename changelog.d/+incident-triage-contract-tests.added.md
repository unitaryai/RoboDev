**Incident-triage contract test suite**: unit and integration tests
pinning the externally observable behaviour of the incident.io webhook
flow (`ProcessIncidentEvent`), covering webhook signature verification,
config parsing, idempotency, TaskRun/Job naming, the ticketing-only
gates the flow deliberately skips (engine selection, cost estimation,
tournaments, approval, memory, notifications, `MarkInProgress`),
completion handling, stream-reader gating, and a golden-file prompt for
the no-repo-URL shape. Ahead of the planned use-case abstraction
refactor, so downstream deployments relying on this flow are not broken
silently. No production code changed.
