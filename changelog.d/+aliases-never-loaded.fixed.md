**Secret aliases declared in config were never loaded.**
`initSecretsResolver` built backends and policy from
`secret_resolver`, but never called `WithAliases`, so the `aliases:`
block was inert. With the fail-closed default (`allow_raw_refs: false`)
permitting only `alias:` references, this meant no task-scoped secret
could resolve at all under the recommended configuration.
