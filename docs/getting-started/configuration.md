# Configuration Reference

RoboDev is configured via a YAML file (`robodev-config.yaml`) which is mounted into the controller pod as a ConfigMap. When deploying with Helm, you set configuration under the `config:` key in your `values.yaml` and the chart creates the ConfigMap for you.

## Top-Level Sections

| Section | Purpose |
|---|---|
| `ticketing` | Where tasks come from (GitHub Issues, GitLab Issues, Jira via plugin) |
| `engines` | Which AI coding agents are available and which is the default |
| `notifications` | Where status updates are sent (Slack, Microsoft Teams via plugin) |
| `secrets` | How the controller retrieves credentials (`k8s` for Kubernetes Secrets) |
| `scm` | Source code management backend for cloning and opening PRs |
| `guardrails` | Safety boundaries — cost limits, concurrency limits, blocked file patterns |
| `tenancy` | Multi-tenancy mode (`shared` or `namespace-per-tenant`) |
| `quality_gate` | Optional AI-powered review of agent output before merging |
| `review` | Review backend configuration |
| `progress_watchdog` | Detects stalled or looping agent jobs and intervenes |
| `plugin_health` | Health monitoring and restart behaviour for gRPC plugins |
| `execution` | Execution backend (`job`, `sandbox`, or `local`) |
| `webhook` | Optional webhook receiver for instant ticket ingestion |
| `secret_resolver` | Task-scoped secret resolution and policy enforcement |
| `streaming` | Real-time agent output streaming configuration |
| `taskrun_store` | Persistent TaskRun store backend (`memory`, `sqlite`, `postgres`) |

For the full set of fields and their defaults, see `charts/robodev/values.yaml` and the struct definitions in [`internal/config/config.go`](https://github.com/unitaryai/robodev/blob/main/internal/config/config.go).

## Ticketing

```yaml
ticketing:
  backend: github          # "github", "gitlab", or a plugin name
  config:
    owner: "your-org"
    repo: "your-repo"
    labels:
      - "robodev"
    token_secret: "robodev-github-token"
```

The ticketing backend is the primary input source. The controller polls it every reconciliation cycle (default: 30 seconds) for tickets matching the configured labels.

## Engines

```yaml
engines:
  default: claude-code     # Default engine for all tasks
  fallback_engines:        # Tried in order if the default fails
    - cline
    - aider
  claude-code:
    auth:
      method: api_key
      api_key_secret: "robodev-anthropic-key"
    fallback_model: haiku
    no_session_persistence: true
  codex:
    auth:
      method: api_key
      api_key_secret: "robodev-openai-key"
  opencode:
    provider: anthropic    # "anthropic", "openai", "google"
    auth:
      method: api_key
      api_key_secret: "robodev-anthropic-key"
  cline:
    provider: anthropic    # "anthropic", "openai", "google", "bedrock"
    mcp_enabled: true
    auth:
      method: api_key
      api_key_secret: "robodev-anthropic-key"
```

See [Engine Comparison](../plugins/engines.md) for detailed per-engine configuration.

### Authentication Methods

| Method | Description |
|---|---|
| `api_key` | API key stored in a Kubernetes Secret |
| `bedrock` | AWS Bedrock via IRSA (IAM Roles for Service Accounts) |
| `vertex` | Google Vertex AI via Workload Identity Federation |
| `credentials_file` | Credentials file mounted from a Kubernetes Secret |
| `setup_token` | Setup token for initial authentication |

## Guard Rails

```yaml
guardrails:
  max_cost_per_job: 50.0              # Maximum USD spend per task
  max_concurrent_jobs: 5              # Concurrent job limit
  max_job_duration_minutes: 120       # Hard timeout for jobs
  allowed_repos:                      # Glob patterns for permitted repos
    - "org/frontend-*"
    - "org/backend-*"
  blocked_file_patterns:              # Files the agent must never modify
    - "*.env"
    - "**/secrets/**"
    - "**/credentials/**"
  require_human_approval_before_mr: false
  allowed_task_types:                 # Restrict to specific task categories
    - "bug_fix"
    - "documentation"
    - "dependency_upgrade"
  task_profiles:                      # Per-task-type permissions
    documentation:
      allowed_file_patterns: ["*.md", "docs/**"]
      max_cost_per_job: 10.0
  approval_gates:                     # Cost thresholds requiring approval
    - "high_cost"
  approval_cost_threshold_usd: 25.0
```

See [Guard Rails](../guardrails.md) for the full specification.

## Notifications

```yaml
notifications:
  channels:
    - backend: slack
      config:
        channel_id: "C0123456789"
        token_secret: "robodev-slack-token"
    - backend: teams
      config:
        webhook_url_secret: "robodev-teams-webhook"
```

Multiple channels can be configured simultaneously. All channels receive all events. Notification failures are logged but do not block the controller.

## Secrets

```yaml
secrets:
  backend: k8s             # "k8s" (built-in) or a plugin name
  config:
    namespace: "robodev"   # Optional — defaults to the controller's namespace
```

For external secret stores, see the [Secrets plugin documentation](../plugins/secrets.md).

## Secret Resolver

The secret resolver provides task-scoped secret resolution with policy enforcement:

```yaml
secret_resolver:
  backends:
    - scheme: k8s
      backend: k8s
    - scheme: vault
      backend: vault
      config:
        address: "https://vault.example.com"
  aliases:
    anthropic-key:
      uri: "k8s://robodev/robodev-anthropic-key/api_key"
  policy:
    allowed_env_patterns: ["ANTHROPIC_*", "OPENAI_*", "GITHUB_*"]
    blocked_env_patterns: ["AWS_SECRET_*"]
    allow_raw_refs: false
    allowed_schemes: ["k8s", "vault"]
```

## Quality Gate

```yaml
quality_gate:
  enabled: true
  mode: "post-completion"          # or "security-only"
  engine: claude-code              # Engine used for reviews
  max_cost_per_review: 5.0
  security_checks:
    scan_for_secrets: true
    check_owasp_patterns: true
    verify_guardrail_compliance: true
    check_dependency_cves: true
  on_failure: "retry_with_feedback"  # or "block_mr", "notify_human"
```

## Progress Watchdog

```yaml
progress_watchdog:
  enabled: true
  check_interval_seconds: 60
  min_consecutive_ticks: 2
  research_grace_period_minutes: 5
  loop_detection_threshold: 10
  thrashing_token_threshold: 80000
  stall_idle_seconds: 300
  cost_velocity_max_per_10_min: 15.0
  unanswered_human_timeout_minutes: 30
```

See [Guard Rails Overview](../concepts/guardrails-overview.md) for an explanation of each detection rule.

## Execution

```yaml
execution:
  backend: job               # "job" (default), "sandbox", or "local"
  sandbox:
    runtime_class: gvisor     # or "kata"
    warm_pool:
      enabled: true
      size: 2
    env_stripping: true
```

## Webhook

```yaml
webhook:
  enabled: true
  port: 8081
  github:
    secret: "your-github-webhook-secret"
  gitlab:
    secret: "your-gitlab-webhook-secret"
  slack:
    secret: "your-slack-signing-secret"
  shortcut:
    secret: "your-shortcut-webhook-secret"
  generic:
    auth_token: "your-bearer-token"
    field_map:
      title: "summary"
      description: "body"
```

## Streaming

```yaml
streaming:
  enabled: true
  live_notifications: true
```

## TaskRun Store

```yaml
taskrun_store:
  backend: memory            # "memory" (default), "sqlite", or "postgres"
  sqlite:
    path: "/data/taskruns.db"
```

## Tenancy

```yaml
tenancy:
  mode: "namespace-per-tenant"   # or "shared"
  tenants:
    - name: "team-alpha"
      namespace: "robodev-alpha"
      ticketing:
        backend: github
        config:
          repo: "alpha-org/repos"
      secrets:
        backend: k8s
```

## Plugin Health

```yaml
plugin_health:
  max_plugin_restarts: 3
  restart_backoff: [1, 5, 30]    # Seconds between restart attempts
  critical_plugins:
    - "ticketing"
```

## SCM

```yaml
scm:
  backend: github
  config:
    token_secret: "robodev-github-token"
```

## Review

```yaml
review:
  backend: coderabbit
  config:
    api_key_secret: "coderabbit-api-key"
```

## Environment Variable Overrides

Configuration values can be overridden via environment variables following the pattern `ROBODEV_<SECTION>_<FIELD>`:

| Variable | Overrides |
|---|---|
| `ROBODEV_TICKETING_BACKEND` | `ticketing.backend` |
| `ROBODEV_ENGINE_DEFAULT` | `engines.default` |
| `ROBODEV_GUARDRAILS_MAX_COST_PER_JOB` | `guardrails.max_cost_per_job` |
| `ROBODEV_GUARDRAILS_MAX_CONCURRENT_JOBS` | `guardrails.max_concurrent_jobs` |
