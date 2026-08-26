**Use-case registry and `TaskRun.UseCase` tagging**: a new
`internal/usecase` package implements the `Definition`/`Gates`/
`ResultHandler` model from `docs/designs/use-case-abstraction.md`
(`ExecutionMode`, `Gates`, `Registry`), registered at reconciler
construction with two Definitions, `ticketing` (all gates on,
`clone_push_mr`) and `incident-triage` (all gates off, `api_read`).
`TaskRun` gains a persisted `UseCase` field, set at creation time for
both `ProcessTicket` and `ProcessIncidentEvent`. A `useCaseFor` shim
infers the use case for TaskRuns persisted before this field existed,
from the `tr-incident-` ID prefix. This PR is inert: nothing consumes
`Gates` or `Results` yet, and completion/failure handling is
unchanged; dispatching through the registry is a follow-up PR.
