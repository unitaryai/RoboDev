**Task-scoped secrets are now actually resolved and injected.** The
`internal/secretresolver` package, its config schema and its policy
enforcement have shipped since the secrets work landed, and `main.go`
built a `Resolver` and handed it to the reconciler via
`WithSecretsResolver`, but nothing ever called `Resolve`, so every
`osmia:secrets` block and `osmia:secret:ENV=URI` label was silently
ignored. The launch pipeline now parses a task's declarations from its
description and labels, resolves them, and merges them into the
`ExecutionSpec` before the Job is built. Resolution is fail-closed: a
policy violation or backend failure aborts the launch rather than
starting an agent without the secret it asked for. `k8s://` references
are injected natively as a `secretKeyRef`; values from other backends
(Vault, AWS Secrets Manager) are staged into an ephemeral Secret named
`osmia-task-secrets-<task-run-id>`, owned by the agent Job so Kubernetes
garbage-collects it with the run, so resolved plaintext never appears in
the Job manifest. A task may not shadow an environment variable the
engine already sets, which stops a ticket author overriding the agent's
own credentials.
