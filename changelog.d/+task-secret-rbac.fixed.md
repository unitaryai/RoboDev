**The Helm chart never granted the RBAC that task-scoped secrets need.** The
controller ClusterRole allowed only `get`, `list` and `watch` on Secrets, but
staging a resolved value into an ephemeral Secret needs `create`, adding the
owning Job's reference needs `update`, and both the abort path and the new
orphan sweep need `delete`. Any chart-deployed controller would have failed
with a 403 the first time a task declared a secret from a non-Kubernetes
backend. The verbs are now granted, with a comment saying which operation
needs each.
