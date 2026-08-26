**Episodic memory was not actually partitioned by tenant.** The graph query
filtered on tenant and the store enforced it, but nothing ever set one: the
extractor hardcoded `TenantID: ""` on every node it produced, and all three
controller query sites passed an empty tenant, which matches everything. The
docs described working multi-tenant isolation throughout. In practice the
ticketing flow and the incident-triage flow shared one undifferentiated pool
of facts, so an incident classification could be offered as prior knowledge
in a code-change prompt.

`TaskRun` now carries a `TenantID`, assigned at creation from the use case
that made it (`ticketing` or `incident-triage`). Extraction stamps every node
with it and the controller's queries filter on it. Partitioning is per use
case rather than per deployment, so it still holds if the two flows are ever
consolidated into one binary.

Facts written before this change carry no tenant and a tenanted query will
not return them. A memory graph re-accumulates within a few runs, so no
migration is provided.

`Graph.Query` still treats an empty query tenant as "match every tenant",
for whole-graph administrative reads. That default is fail-open, so it is now
documented as such at both the field and the filter, and every controller
query passes a tenant.
