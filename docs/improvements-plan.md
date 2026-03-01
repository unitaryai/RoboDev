# Improvements Plan

This document tracks planned features and enhancements beyond the original `oss-plan.md` scope. Each item includes rationale, design notes, and implementation guidance.

---

## 1. Webhook Receiver & Event-Driven Ingestion

**Status:** Planned
**Priority:** Critical (blocks production use)
**Depends on:** Controller (`internal/controller/`)

### Rationale

RoboDev currently polls ticketing backends every 30–60 seconds. This means sub-second response times are impossible, and approval callbacks (Slack interactive messages) have no HTTP endpoint to land on. Every production deployment of a ticket-driven system needs event-driven ingestion.

### Design

Add an HTTP server to the controller that receives webhooks from ticketing backends and approval channels. The webhook handler validates signatures, extracts ticket events, and feeds them into the existing reconciliation loop.

**Package location:** `internal/webhook/`

**Endpoints:**
| Path | Source | Purpose |
|------|--------|---------|
| `POST /webhooks/github` | GitHub | Issue created/labelled/commented events |
| `POST /webhooks/gitlab` | GitLab | Issue/MR webhook events |
| `POST /webhooks/shortcut` | Shortcut | Story state change events |
| `POST /webhooks/slack/interaction` | Slack | Approval button callbacks |
| `POST /webhooks/generic` | Any | Generic JSON payload with configurable field mapping |

**Security:**
- GitHub: HMAC-SHA256 signature validation (`X-Hub-Signature-256`)
- GitLab: secret token validation (`X-Gitlab-Token`)
- Slack: signing secret validation (`X-Slack-Signature` + `X-Slack-Request-Timestamp`)
- Generic: configurable HMAC or bearer token

**Helm chart changes:**
- Add Ingress template (disabled by default)
- Add Service for webhook port (separate from metrics)
- Add NetworkPolicy allowing ingress on webhook port

### Implementation checklist

- [ ] Create `internal/webhook/server.go` — HTTP server with route registration
- [ ] Create `internal/webhook/github.go` — GitHub webhook signature validation and event parsing
- [ ] Create `internal/webhook/gitlab.go` — GitLab webhook handler
- [ ] Create `internal/webhook/slack.go` — Slack interaction payload handler
- [ ] Create `internal/webhook/generic.go` — Generic webhook with configurable field mapping
- [ ] Wire webhook server into `cmd/robodev/main.go` (opt-in via config)
- [ ] Add Ingress template to Helm chart
- [ ] Add webhook Service template to Helm chart
- [ ] Update `docs/getting-started.md` with webhook setup instructions
- [ ] Write tests for signature validation and event parsing

---

## 2. Agent Sandbox Integration (gVisor / Warm Pools)

**Status:** Implemented (core + testing; alpha API may change)
**Priority:** Critical (security + performance)
**Depends on:** `kubernetes-sigs/agent-sandbox` controller

### Rationale

RoboDev currently runs agents in standard K8s Jobs with hardened security contexts (runAsNonRoot, readOnlyRootFilesystem, dropped capabilities). This provides process-level isolation but shares the host kernel. If an agent executes malicious LLM-generated code, a container escape could compromise the node.

The [`kubernetes-sigs/agent-sandbox`](https://github.com/kubernetes-sigs/agent-sandbox) project provides a Kubernetes-native solution: a declarative API for managing isolated agent pods with gVisor (user-space kernel) or Kata Containers (micro-VM) runtime classes. It also offers a **warm pool CRD** that maintains pre-warmed pods, reducing cold start from ~30 seconds (image pull + pod scheduling) to under one second.

Browser Use's architecture (see [Larsen Cundric's article](https://x.com/larsencc/status/2027225210412470668)) validates this pattern in production at scale — they run millions of agents in Unikraft micro-VMs with the same "isolate the agent" model RoboDev uses.

### Design

RoboDev already follows the "Pattern 2: Isolate the agent" approach — agents run in disposable pods with no infrastructure credentials, and the controller holds all secrets. Agent-sandbox extends this with kernel-level isolation.

**Integration approach:**
1. Add an optional `sandbox` execution backend alongside the existing `job` backend
2. When enabled, the JobBuilder creates `Sandbox` CRs instead of `batch/v1.Job` resources
3. The warm pool maintains pre-provisioned pods with engine images ready to go
4. Agent pods use gVisor (`runsc`) runtime class by default, with Kata Containers as an option

**Configuration:**
```yaml
execution:
  backend: sandbox  # "job" (default) or "sandbox"
  sandbox:
    runtime_class: gvisor  # gvisor | kata
    warm_pool:
      enabled: true
      size: 3              # pre-warmed pods per engine
    env_stripping: true    # delete env vars from os.environ after reading (Browser Use pattern)
```

**Environment stripping** (inspired by Browser Use): Engine entrypoints should read `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` into memory, then delete the variables from the process environment. If the agent inspects `os.environ` or runs `env`, credentials are gone.

### Implementation checklist

- [ ] Add `agent-sandbox` as an optional dependency in the Helm chart
- [ ] Extend `internal/jobbuilder/` to emit `Sandbox` CRs when `backend: sandbox`
- [ ] Add gVisor RuntimeClass to Helm chart templates
- [ ] Add `SandboxWarmPool` CR template for warm pool configuration
- [ ] Implement environment stripping in engine entrypoint scripts
- [ ] Update security documentation with gVisor isolation model
- [ ] Add example configuration for sandbox mode
- [ ] Write integration tests against a kind cluster with gVisor

---

## 3. OpenCode Execution Engine

**Status:** Planned
**Priority:** High
**Depends on:** Existing `ExecutionEngine` interface (`pkg/engine/engine.go`)

### Rationale

OpenCode has become the fastest-growing open-source coding agent (95K+ GitHub stars as of February 2026, growing from 39K to 72K in a single month). It follows the BYOM (Bring Your Own Model) pattern, allowing operators to choose their LLM provider. Adding OpenCode broadens community appeal and gives users a fourth engine option alongside Claude Code, Codex, and Aider.

### Design

OpenCode is a terminal-native agent, making it a natural fit for RoboDev's headless K8s Job execution model.

**Package location:** `pkg/engine/opencode/`

**Key characteristics:**
- BYOM — supports Anthropic, OpenAI, Google, and local models via provider configuration
- Terminal-native CLI, runs headless without an IDE
- No hooks system — guard rails via prompt-based instructions (same adapter pattern as Aider/Codex, see `oss-plan.md` section 6.3)
- Repository context via `AGENTS.md` (same as Codex)

**Invocation (expected):**
```bash
opencode --non-interactive \
  --message "$(cat /config/task-prompt.md)"
```

**Container image:** `ghcr.io/unitaryai/engine-opencode:latest`

**Struct design:**
```go
type OpenCodeEngine struct {
    provider ModelProvider  // anthropic, openai, google, local
}
```

**Provider/secret mapping:**
| Provider  | Environment variable  | K8s secret key       |
|-----------|-----------------------|----------------------|
| Anthropic | `ANTHROPIC_API_KEY`   | `anthropic-api-key`  |
| OpenAI    | `OPENAI_API_KEY`      | `openai-api-key`     |
| Google    | `GOOGLE_API_KEY`      | `google-api-key`     |

### Implementation checklist

- [ ] Create `pkg/engine/opencode/engine.go` implementing `ExecutionEngine`
- [ ] Create `pkg/engine/opencode/engine_test.go` with table-driven tests
- [ ] Add OpenCode Dockerfile (`docker/engine-opencode/Dockerfile`)
- [ ] Register engine in `pkg/engine/registry.go`
- [ ] Add Makefile target `docker-build-engine-opencode`
- [ ] Add example configuration in `examples/`
- [ ] Update `docs/plugins/engines.md` with OpenCode documentation
- [ ] Update Helm chart `values.yaml` with OpenCode engine config

---

## 4. Cline CLI Execution Engine

**Status:** Planned
**Priority:** High
**Depends on:** Existing `ExecutionEngine` interface (`pkg/engine/engine.go`)

### Rationale

Cline (formerly Claude Dev) shipped CLI 2.0 with a headless CI/CD mode in February 2026 — purpose-built for autonomous pipelines, which maps directly to RoboDev's execution model. With 58K GitHub stars, 5.8M VS Code installs, and 297 contributors, it has one of the largest communities in the AI coding space. Cline follows the BYOM pattern and supports MCP, making it the most feature-rich terminal agent after Claude Code.

### Design

Cline CLI 2.0's headless mode runs without a UI, outputs structured JSON, and supports MCP servers — making it an excellent fit for K8s Job execution.

**Package location:** `pkg/engine/cline/`

**Key characteristics:**
- BYOM — supports Anthropic, OpenAI, Google, AWS Bedrock, and other providers
- Headless CI/CD mode (`cline --headless`) designed for pipeline use
- MCP support — can use RoboDev's existing MCP servers for notifications and human interaction
- Subagent support — native parallel execution with dedicated context windows (v3.58+)
- No hooks system — guard rails via prompt-based instructions
- Repository context via `.clinerules` file

**Invocation (expected):**
```bash
cline --headless \
  --task "$(cat /config/task-prompt.md)" \
  --output-format json
```

**Container image:** `ghcr.io/unitaryai/engine-cline:latest`

**Struct design:**
```go
type ClineEngine struct {
    provider ModelProvider  // anthropic, openai, google, bedrock
    mcpEnabled bool        // whether to wire up RoboDev MCP servers
}
```

**Provider/secret mapping:**
| Provider  | Environment variable  | K8s secret key       |
|-----------|-----------------------|----------------------|
| Anthropic | `ANTHROPIC_API_KEY`   | `anthropic-api-key`  |
| OpenAI    | `OPENAI_API_KEY`      | `openai-api-key`     |
| Google    | `GOOGLE_API_KEY`      | `google-api-key`     |
| Bedrock   | (uses IRSA/WIF)      | n/a                  |

**MCP integration note:** Cline's MCP support means it can natively connect to RoboDev's stdio MCP servers (`notify`, `ask_human`, `wait_for_pipeline`), giving it capabilities closer to Claude Code than other hookless engines. This should be explored as a differentiator.

### Implementation checklist

- [ ] Create `pkg/engine/cline/engine.go` implementing `ExecutionEngine`
- [ ] Create `pkg/engine/cline/engine_test.go` with table-driven tests
- [ ] Add Cline Dockerfile (`docker/engine-cline/Dockerfile`)
- [ ] Register engine in `pkg/engine/registry.go`
- [ ] Add Makefile target `docker-build-engine-cline`
- [ ] Investigate MCP server wiring in headless mode
- [ ] Add example configuration in `examples/`
- [ ] Update `docs/plugins/engines.md` with Cline documentation
- [ ] Update Helm chart `values.yaml` with Cline engine config

---

## 5. Shortcut.com Ticketing Backend

**Status:** Planned
**Priority:** High
**Depends on:** `pkg/plugin/ticketing/` interface

### Rationale

Shortcut (formerly Clubhouse) is widely used by engineering teams, particularly in the mid-market segment RoboDev targets. The `oss-plan.md` Phase 6 references a Python `robodev-plugin-shortcut` for Unitary's internal migration, but a built-in Go implementation would benefit the broader community and avoid the external plugin overhead.

### Design

**Package location:** `pkg/plugin/ticketing/shortcut/`

**Shortcut API integration:**
- REST API v3 (`https://api.app.shortcut.com/api/v3/`)
- Authentication via `Shortcut-Token` header
- Poll stories by workflow state, label, or search query
- Webhook support for story state transitions (`POST /webhooks/shortcut`)

**Story-to-ticket mapping:**
| Shortcut field | RoboDev Ticket field |
|----------------|---------------------|
| `story.id` | `ID` |
| `story.name` | `Title` |
| `story.description` | `Body` |
| `story.labels[].name` | `Labels` |
| `story.app_url` | `URL` |
| `story.updated_at` | `UpdatedAt` |

**Configuration:**
```yaml
ticketing:
  backend: shortcut
  shortcut:
    api_token_secret: shortcut-api-token
    workspace: my-workspace
    workflow_state: "Ready for RoboDev"  # poll stories in this state
    labels: ["robodev"]                  # optional label filter
```

### Implementation checklist

- [ ] Create `pkg/plugin/ticketing/shortcut/shortcut.go` implementing `ticketing.Backend`
- [ ] Create `pkg/plugin/ticketing/shortcut/shortcut_test.go`
- [ ] Add Shortcut webhook handler to `internal/webhook/shortcut.go`
- [ ] Add Shortcut configuration to config schema
- [ ] Add example values for Shortcut deployment
- [ ] Update ticketing plugin documentation

---

## 6. Telegram Notification Channel

**Status:** Planned
**Priority:** High
**Depends on:** `pkg/plugin/notifications/` interface

### Rationale

Telegram is widely used by developer communities, open-source projects, and teams outside the Slack ecosystem. Its Bot API is simple (single HTTP endpoint), making it a lightweight notification channel. Good for individual developers and smaller teams who want RoboDev notifications without a Slack workspace.

### Design

**Package location:** `pkg/plugin/notifications/telegram/`

**Telegram Bot API integration:**
- Send messages via `POST https://api.telegram.org/bot{token}/sendMessage`
- Support Markdown formatting for structured notifications
- Support chat IDs (direct messages) and group chat IDs
- Optional: inline keyboard buttons for approval workflows

**Configuration:**
```yaml
notifications:
  channels:
    - backend: telegram
      telegram:
        bot_token_secret: telegram-bot-token
        chat_id: "-1001234567890"  # group chat or user ID
        thread_id: 42              # optional: message thread (topics)
```

### Implementation checklist

- [ ] Create `pkg/plugin/notifications/telegram/telegram.go` implementing `notifications.Channel`
- [ ] Create `pkg/plugin/notifications/telegram/telegram_test.go`
- [ ] Add Telegram configuration to config schema
- [ ] Update notification channel documentation

---

## 7. Linear Ticketing Backend

**Status:** Planned
**Priority:** Medium
**Depends on:** `pkg/plugin/ticketing/` interface

### Rationale

Linear is the most popular project management tool among the engineering teams likely to adopt RoboDev — fast-moving startups and developer-tool companies. Its GraphQL API is well-documented and supports webhooks natively.

### Design

**Package location:** `pkg/plugin/ticketing/linear/`

**Linear API integration:**
- GraphQL API (`https://api.linear.app/graphql`)
- Authentication via `Authorization: Bearer` header
- Query issues by team, label, state, or cycle
- Webhook support for issue state transitions

**Configuration:**
```yaml
ticketing:
  backend: linear
  linear:
    api_key_secret: linear-api-key
    team_id: "TEAM-123"
    state: "Ready for RoboDev"
    labels: ["robodev"]
```

### Implementation checklist

- [ ] Create `pkg/plugin/ticketing/linear/linear.go` implementing `ticketing.Backend`
- [ ] Create `pkg/plugin/ticketing/linear/linear_test.go`
- [ ] Add Linear webhook handler to `internal/webhook/linear.go`
- [ ] Add Linear configuration to config schema
- [ ] Update ticketing plugin documentation

---

## 8. Discord Notification Channel

**Status:** Planned
**Priority:** Medium
**Depends on:** `pkg/plugin/notifications/` interface

### Rationale

Discord has become the default community platform for open-source projects. Its webhook API is trivially simple (POST JSON to a URL, no authentication library needed). Supporting Discord makes RoboDev accessible to OSS communities that coordinate on Discord rather than Slack.

### Design

**Package location:** `pkg/plugin/notifications/discord/`

**Discord integration:**
- Webhook URL-based (no bot token required for basic notifications)
- Rich embed support for structured task status messages
- Optional: bot token integration for interactive approval workflows

**Configuration:**
```yaml
notifications:
  channels:
    - backend: discord
      discord:
        webhook_url_secret: discord-webhook-url  # simple webhook mode
        # OR bot mode for interactive features:
        bot_token_secret: discord-bot-token
        channel_id: "1234567890"
```

### Implementation checklist

- [ ] Create `pkg/plugin/notifications/discord/discord.go` implementing `notifications.Channel`
- [ ] Create `pkg/plugin/notifications/discord/discord_test.go`
- [ ] Add Discord configuration to config schema
- [ ] Update notification channel documentation

---

## 9. HashiCorp Vault Secrets Backend

**Status:** Planned
**Priority:** Medium
**Depends on:** `pkg/plugin/secrets/` interface

### Rationale

Enterprise deployments frequently mandate Vault for secrets management. The current K8s Secrets backend is sufficient for smaller deployments, but production environments need Vault integration for audit trails, dynamic secrets, and centralised credential rotation.

### Design

**Package location:** `pkg/plugin/secrets/vault/`

**Vault integration:**
- Kubernetes auth method (pod ServiceAccount → Vault token, no static credentials)
- KV v2 secrets engine for static secrets
- Optional: dynamic database credentials for agent workspace databases

**Configuration:**
```yaml
secrets:
  backend: vault
  vault:
    address: "https://vault.internal:8200"
    auth_method: kubernetes  # kubernetes | approle
    role: robodev-controller
    secrets_path: "secret/data/robodev"
```

### Implementation checklist

- [ ] Create `pkg/plugin/secrets/vault/vault.go` implementing `secrets.Backend`
- [ ] Create `pkg/plugin/secrets/vault/vault_test.go`
- [ ] Add Vault configuration to config schema
- [ ] Document Vault setup (policy, role, auth method)

---

## 10. Task-Scoped Secret Resolution

**Status:** Planned
**Priority:** Critical (blocks multi-tenant and multi-repo use)
**Depends on:** `pkg/plugin/secrets/` interface, `pkg/plugin/ticketing/` Ticket struct, `internal/jobbuilder/`

### Problem

Today, secrets are engine-scoped: Claude Code always gets `anthropic-api-key`, Codex always gets `openai-api-key`. Every task running on the same engine receives the same credentials. This breaks down in real-world scenarios:

1. **Multi-tenant** — Tenant A and Tenant B each bring their own LLM API key
2. **Multi-repo** — Repo X needs a GitHub token scoped to org-x, Repo Y needs one scoped to org-y
3. **Task-specific credentials** — A deployment task needs cloud credentials; a pure code task doesn't
4. **BYOM engines** — Aider/OpenCode/Cline could use Anthropic for one task and OpenAI for another
5. **Third-party services** — A task needs a Stripe test key, a Sentry DSN, or a database URL that varies per project

The person creating the ticket in the ticketing system needs a way to declare what secrets the agent will need, and the controller needs to resolve those declarations securely before launching the job.

### Design Principles

1. **Ticket authors declare intent, not values.** The ticket says "I need the Stripe test key for project-X", not the actual key. This is a *reference*, not a *secret*.
2. **The controller resolves references against an allowlist.** Not every secret in Vault or AWS SM is available — only those pre-approved in the RoboDev config. This prevents ticket-driven exfiltration.
3. **Multiple backends, unified reference format.** A single reference syntax works across K8s Secrets, Vault, AWS Secrets Manager, 1Password, etc. The controller routes to the correct backend.
4. **Least privilege by default.** If a ticket doesn't declare secrets, the agent gets only the engine's baseline credentials (LLM API key). Additional secrets are opt-in.
5. **Audit trail.** Every secret injection is logged (secret name, not value) with the task ID, ticket ID, and requesting user.

### Secret Reference Format

Ticket authors use a structured annotation in the ticket body (parsed by the controller, not the agent):

```
<!-- robodev:secrets
  - ref: vault://secret/data/stripe/test-key#api_key
    env: STRIPE_API_KEY
  - ref: aws-sm://production/database-url
    env: DATABASE_URL
  - ref: k8s://my-namespace/github-token/token
    env: GH_TOKEN
  - ref: 1password://vault-name/item-name/field
    env: SENTRY_DSN
-->
```

**Reference URI scheme:**
| Scheme | Backend | Example |
|--------|---------|---------|
| `k8s://` | Kubernetes Secrets | `k8s://namespace/secret-name/data-key` |
| `vault://` | HashiCorp Vault | `vault://secret/data/path#field` |
| `aws-sm://` | AWS Secrets Manager | `aws-sm://secret-name` or `aws-sm://secret-name#json-key` |
| `1password://` | 1Password Connect | `1password://vault/item/field` |
| `alias://` | Pre-defined alias (see below) | `alias://stripe-test` |

**Why HTML comments?** They're invisible in rendered ticket views (GitHub, Linear, Shortcut all strip them from display), so they don't clutter the ticket for human readers. They're also unambiguous to parse — no risk of the agent or an LLM misinterpreting them as instructions.

**Alternative: Labels/custom fields.** Some ticketing systems support custom fields (Jira, Linear, Shortcut). For these, secrets could be declared in a structured custom field rather than an HTML comment. The controller should support both mechanisms, with the parsing strategy configurable per ticketing backend.

### Secret Aliases (Pre-Approved Bundles)

For common patterns, administrators define aliases in the RoboDev config that map a short name to one or more backend-specific references. This is the primary security control — ticket authors can only reference aliases that the admin has pre-approved.

```yaml
secrets:
  backend: multi  # enables multi-backend resolution
  backends:
    - name: k8s
      type: kubernetes
      namespace: robodev-secrets
    - name: vault
      type: vault
      address: "https://vault.internal:8200"
      auth_method: kubernetes
      role: robodev-controller
    - name: aws
      type: aws-secretsmanager
      region: eu-west-1

  # Pre-approved secret aliases that ticket authors can reference
  aliases:
    stripe-test:
      description: "Stripe test API key for integration tests"
      refs:
        - ref: vault://secret/data/stripe/test#api_key
          env: STRIPE_API_KEY
    prod-db-readonly:
      description: "Read-only production database credentials"
      refs:
        - ref: aws-sm://prod/db-readonly-url
          env: DATABASE_URL
    github-org-x:
      description: "GitHub token scoped to org-x"
      refs:
        - ref: k8s://robodev-secrets/github-org-x/token
          env: GH_TOKEN

  # Security policy
  policy:
    allow_raw_refs: false     # if false, only aliases are permitted (recommended)
    require_approval: false   # if true, secret requests need human approval before injection
    allowed_env_names:        # restrict which env var names can be set (prevents PATH hijack etc.)
      - "STRIPE_*"
      - "DATABASE_URL"
      - "GH_TOKEN"
      - "SENTRY_DSN"
      - "AWS_*"
      - "*_API_KEY"
    blocked_env_names:        # always blocked, even if matched by allowed pattern
      - "PATH"
      - "HOME"
      - "LD_PRELOAD"
      - "LD_LIBRARY_PATH"
      - "ROBODEV_*"          # prevent agents from impersonating controller internals
```

### Ticket-Level Usage

**Using aliases (recommended, `allow_raw_refs: false`):**
```
<!-- robodev:secrets
  - alias: stripe-test
  - alias: github-org-x
-->
```

**Using raw references (when `allow_raw_refs: true`):**
```
<!-- robodev:secrets
  - ref: vault://secret/data/myapp/config#api_key
    env: MY_API_KEY
-->
```

**Using labels (alternative for ticketing systems with structured metadata):**
Apply labels like `robodev:secret:stripe-test` and `robodev:secret:github-org-x` to the ticket. The controller recognises the `robodev:secret:` prefix and resolves the alias.

### Per-Tenant Secret Scoping

In multi-tenant mode, each tenant has its own set of allowed aliases. A tenant can only reference aliases defined in their tenant config, not another tenant's:

```yaml
tenancy:
  mode: namespace-per-tenant
  tenants:
    - name: team-alpha
      namespace: robodev-alpha
      secrets:
        aliases:
          stripe-test:
            refs:
              - ref: k8s://robodev-alpha/stripe-test/api_key
                env: STRIPE_API_KEY
    - name: team-beta
      namespace: robodev-beta
      secrets:
        aliases:
          stripe-test:  # same alias name, different backing secret
            refs:
              - ref: vault://secret/data/team-beta/stripe#key
                env: STRIPE_API_KEY
```

### Resolution Flow

```
1. Controller polls/receives ticket
2. Parse ticket body for <!-- robodev:secrets --> block (or check labels/custom fields)
3. Resolve aliases → list of (backend, ref, env_name) tuples
4. Validate against policy:
   a. If allow_raw_refs=false, reject any non-alias references
   b. Validate env_name against allowed_env_names / blocked_env_names
   c. If require_approval=true, pause task and request human approval
5. For each secret reference, call the appropriate secrets.Backend:
   a. k8s:// → K8sBackend.GetSecret()
   b. vault:// → VaultBackend.GetSecret()
   c. aws-sm:// → AWSBackend.GetSecret()
   d. 1password:// → OnePasswordBackend.GetSecret()
6. Inject resolved secrets into ExecutionSpec.SecretEnv
   (or use BuildEnvVars for K8s-native secretKeyRef where possible)
7. Log: "Injected secrets [STRIPE_API_KEY, GH_TOKEN] for task {id} from ticket {ticket_id}"
   (names only, never values)
8. Launch job
```

### Threat Model

| Threat | Mitigation |
|--------|------------|
| Ticket author requests secrets they shouldn't have | `allow_raw_refs: false` restricts to admin-defined aliases; tenant scoping limits to tenant's own aliases |
| Prompt injection in ticket body tricks parser | HTML comment parsing is deterministic (regex/string match), not LLM-interpreted; malformed blocks are rejected |
| Agent reads injected env vars and exfiltrates them | Environment stripping (section 2) deletes vars after engine reads them; NetworkPolicy blocks non-allowlisted egress; guard rails block `env` / `printenv` commands |
| Env var name injection (e.g. `PATH`, `LD_PRELOAD`) | `blocked_env_names` list prevents dangerous variable names; `allowed_env_names` provides additional allowlist control |
| Cross-tenant secret access | Tenant aliases are scoped per-tenant; namespace isolation ensures K8s Secrets are unreachable across namespaces |
| Secret sprawl / unused permissions | Audit log tracks every injection; `require_approval` mode forces human sign-off for sensitive environments |

### Implementation Checklist

- [ ] Add `SecretRequest` struct to `pkg/plugin/ticketing/` (parsed from ticket body)
- [ ] Create `internal/secretresolver/resolver.go` — multi-backend secret resolution
- [ ] Create `internal/secretresolver/parser.go` — HTML comment / label / custom field parsing
- [ ] Create `internal/secretresolver/policy.go` — alias validation, env name allowlist/blocklist
- [ ] Add alias and policy config to `internal/config/config.go`
- [ ] Wire resolver into controller reconciliation loop (between ticket poll and job creation)
- [ ] Add audit logging for secret injections
- [ ] Create `pkg/plugin/secrets/awssm/` — AWS Secrets Manager backend
- [ ] Create `pkg/plugin/secrets/onepassword/` — 1Password Connect backend
- [ ] Update `pkg/plugin/secrets/vault/` to support the URI reference format
- [ ] Add `require_approval` flow (integration with approval backend)
- [ ] Write table-driven tests for parser, resolver, and policy validation
- [ ] Add documentation with examples for each ticketing system
- [ ] Add example alias configs for common use cases

---

## 11. NetworkPolicy & Security Hardening

**Status:** Planned
**Priority:** High
**Depends on:** Helm chart (`charts/robodev/`)

### Rationale

The security documentation describes network-level isolation but the Helm chart doesn't generate NetworkPolicy resources. Agent pods should have egress restricted to only the services they need (SCM API, LLM API, controller). This is a gap in the current security posture.

### Design

**NetworkPolicy for agent pods:**
- Egress allowed: controller webhook port, SCM API (github.com/gitlab.com), LLM API endpoints
- Egress denied: everything else (internal services, metadata endpoint, other namespaces)
- All ingress denied (agents don't serve traffic)

**NetworkPolicy for controller:**
- Ingress allowed: webhook port from Ingress controller, metrics port from Prometheus
- Egress allowed: ticketing API, notification API, K8s API server

### Implementation checklist

- [ ] Add NetworkPolicy template for agent pods to Helm chart
- [ ] Add NetworkPolicy template for controller to Helm chart
- [ ] Add PodDisruptionBudget template for controller
- [ ] Make egress allowlist configurable in `values.yaml`
- [ ] Update security documentation with NetworkPolicy examples
- [ ] Test with Calico/Cilium network plugin in kind cluster

---

## 12. Plugin SDKs

**Status:** Planned
**Priority:** Medium
**Depends on:** Protobuf definitions (`proto/`)

### Rationale

Documentation references Python, Go, and TypeScript plugin SDKs, but they don't exist yet. Third-party plugin authors need these SDKs to build integrations without implementing raw gRPC themselves.

### Design

SDKs are generated from protobuf definitions and provide:
- Pre-built gRPC stubs with the hashicorp/go-plugin handshake
- Base classes/interfaces for each plugin type
- Helper utilities for logging, health checks, and configuration
- Example plugins for reference

**Repositories:**
- `unitaryai/robodev-plugin-sdk-python` — `pip install robodev-plugin-sdk`
- `unitaryai/robodev-plugin-sdk-go` — `go get github.com/unitaryai/robodev-plugin-sdk-go`
- `unitaryai/robodev-plugin-sdk-ts` — `npm install @robodev/plugin-sdk`

### Implementation checklist

- [ ] Generate Python gRPC stubs from protobuf definitions
- [ ] Build Python SDK with base classes and hashicorp/go-plugin compatibility
- [ ] Generate Go gRPC stubs (separate module from main controller)
- [ ] Build Go SDK with interface wrappers
- [ ] Generate TypeScript gRPC stubs
- [ ] Build TypeScript SDK with base classes
- [ ] Publish example plugins using each SDK
- [ ] Add SDK documentation to `docs/plugins/`

---

## 13. Local Development Mode

**Status:** Planned
**Priority:** Medium
**Depends on:** Controller refactoring

### Rationale

RoboDev currently requires a Kubernetes cluster for any usage. This is a barrier for evaluating the project, developing plugins, and running in CI. A Docker Compose mode would let users try RoboDev with a single `docker compose up`.

### Design

Provide a `docker-compose.yaml` that runs:
- Controller (with polling, no K8s dependency)
- A single engine container (Claude Code by default)
- Mock ticketing backend (reads from a local YAML file of tasks)

The controller would need a `local` execution backend that uses Docker API instead of the K8s Job API. This is similar to the Browser Use approach where the same image runs as a Docker container locally and in production.

**Configuration:**
```yaml
execution:
  backend: local  # "job" (K8s), "sandbox" (agent-sandbox), or "local" (Docker)
```

### Implementation checklist

- [ ] Create `internal/jobbuilder/docker.go` — Docker execution backend
- [ ] Create `docker-compose.yaml` with controller + engine
- [ ] Create mock ticketing backend for local testing
- [ ] Add `make local-up` / `make local-down` targets
- [ ] Write quickstart guide for Docker Compose mode

---

## 14. Engine Comparison Matrix

Updated engine comparison including the two new engines:

| Capability              | Claude Code | Codex   | Aider   | OpenCode | Cline CLI |
|-------------------------|-------------|---------|---------|----------|-----------|
| Guard rails             | Hooks       | Prompt  | Prompt  | Prompt   | Prompt    |
| MCP support             | Yes         | No      | No      | No       | Yes       |
| Agent teams             | Yes (exp.)  | No      | No      | No       | Yes (native subagents) |
| BYOM                    | No          | No      | Yes     | Yes      | Yes       |
| Repository context file | `CLAUDE.md` | `AGENTS.md` | `.aider/conventions.md` | `AGENTS.md` | `.clinerules` |
| Headless mode           | `--print`   | `--quiet` | `--yes` | `--non-interactive` | `--headless` |
| Structured output       | JSON        | JSON    | No      | TBD      | JSON      |
| Licence                 | Proprietary | Proprietary | Apache 2.0 | MIT | Apache 2.0 |

---

## 15. Future Considerations

These items are not yet planned but may be worth investigating:

- **Pi Coding Agent engine** (`badlogic/pi-mono`) — smaller community than OpenCode/Cline but has a unique RPC mode (JSON over stdin/stdout) that enables structured bidirectional communication during execution. Could give better real-time observability than any other engine including Claude Code. BYOM, headless CLI (`pi -p "query" --mode json`), and a "run in a container" philosophy that aligns perfectly with RoboDev. Worth investigating if the RPC protocol can replace heartbeat-based progress monitoring.
- **Roo Code engine** — gaining reputation for handling complex tasks where other agents struggle; worth monitoring adoption trajectory
- **Kilo Code engine** — BYOM agent with $8M funding; early stage but growing quickly
- **Windsurf engine** — strong free tier but IDE-integrated; not suitable for headless execution unless a CLI mode is released
- **Engine auto-selection** — use task complexity heuristics or historical success rates to automatically pick the best engine for a given ticket
- **Engine fallback chains** — if the primary engine fails, retry with a different engine before marking the task as failed
- **LLM conversation state in controller** — Browser Use keeps full conversation history in the control plane, making agents stateless and resumable across restarts; worth exploring for long-running tasks
- **Presigned URL file sync** — instead of mounting workspace volumes, agents upload/download artefacts via presigned URLs; removes need for shared storage and allows cross-region execution
- **OpenTelemetry tracing** — distributed traces across controller → job → agent for debugging complex task runs
- **Web dashboard** — lightweight control plane UI for inspecting run history, logs, and manual task triggers (could be a community contribution)
- **Jira built-in backend** — currently an example Python plugin; may warrant a built-in Go implementation given Jira's enterprise prevalence
- **PagerDuty approval backend** — for on-call approval workflows in incident response contexts
- **AWS Secrets Manager / GCP Secret Manager backends** — cloud-native alternatives to Vault
