**An alias could not name its target environment variable.** A task
referencing an alias without naming a variable fell back to the alias's
own key, so the documented `anthropic-key` alias tried to inject a
variable of that name and failed `allowed_env_patterns`. `AliasConfig`
gains an `env` field, carried through to `SecretAlias.EnvName`.
