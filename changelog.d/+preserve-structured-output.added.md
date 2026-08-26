**The agent's full `structured_output` is preserved.** Previously, when a run
configured a JSON schema, `ParseEvent` merged `structured_output` into
`ResultEvent` through that struct's own typed fields and then discarded the
original, so any field a schema declared beyond the handful `ResultEvent`
knows about was silently dropped. That made `ResultEvent` an implicit ceiling
on what any flow's output schema could contain. `ResultEvent` and
`engine.TaskResult` now carry `RawStructured`, the whole object as raw JSON,
captured before the typed merge and threaded through to `TaskRun.Result`. The
typed merge itself is unchanged, so existing consumers see exactly what they
saw before.
`RawStructured` is not serialised on `ResultEvent`, to avoid re-emitting the
payload on every forwarded stream event. Ported from the First Responder
fork, where it was written to carry an incident classification.
