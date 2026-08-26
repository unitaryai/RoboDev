**Design doc for the use-case abstraction and shadow mode**:
`docs/designs/use-case-abstraction.md` specifies a `Definition`/`Gates`/
`ResultHandler` model for generalising `ProcessTicket` and
`ProcessIncidentEvent` behind a shared dispatch pipeline, an
execution-mode taxonomy (`clone_push_mr`/`read_only`/`api_read`), a
result-handler taxonomy that fixes the incident-UUID
`MarkComplete`/`MarkFailed` warts, and a shadow-mode design with an
honest assessment of which enforcement layers are hard controls versus
aspirational. Fulfils the design-doc requirement in
[roadmap item 24](docs/roadmap.md#24-non-standard-task-types-analysis-reporting-review).
No behaviour changes; this is a design document only.
