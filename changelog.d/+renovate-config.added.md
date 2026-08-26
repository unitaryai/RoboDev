**Dependency updates are now configured, via Renovate.** The repository had
no dependency automation at all: no Dependabot, no Renovate, and no bot pull
request had ever been opened against it, which is how the agent image came to
sit on a GitLab CLI release from March. `.github/renovate.json5` enables the
Dockerfile, Go module, GitHub Actions and custom-regex managers, holds each
release for seven days before proposing it, and excludes the Go toolchain
itself so a language bump still needs a person. A custom manager tracks the
`GLAB_VERSION` pin in the engine image, which no built-in manager can see
because it is an `ARG` consumed by `curl`. There is deliberately no workflow
alongside it: this runs as the Renovate GitHub App, so a public repository
needs no long-lived token in its secrets and forks inherit nothing that
cannot authenticate.
