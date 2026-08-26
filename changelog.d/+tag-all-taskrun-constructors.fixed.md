**Four launch paths built TaskRuns without a use case or a memory tenant.**
Review follow-ups, tournament candidates, the tournament judge and the
repo-URL poll all construct a `TaskRun` directly rather than going through
the shared launch pipeline, so none of them were tagged. Their extracted
facts were written under no tenant, where no tenanted query returns them:
nothing errored, and the knowledge was quietly lost. The `UseCase` field
added alongside the use-case registry had the same gap, masked until now by
the legacy inference that treats an untagged TaskRun as ticketing. All four
now call a single `tagTaskRun` helper that sets both fields together.
`extractMemory` warns when it is handed an untenanted TaskRun, so a launch
path added later that skips the helper says so in the logs instead of
silently discarding what it learned.
