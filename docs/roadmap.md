# RoboDev Feature Roadmap

This document tracks the strategic feature roadmap for RoboDev. It complements `docs/improvements-plan.md` (which covers the initial 13 items) with the next wave of features to position RoboDev at the leading edge of agentic harnesses.

Tick items off as they are implemented and merged.

---

## Status Legend

- [ ] Not started
- [x] Complete
- **In Progress** — marked in the item description when actively being worked on

---

## Improvements Plan (Original 13 Items)

> Tracked in detail in `docs/improvements-plan.md`. Summary status here.

- [x] **1. Webhook Receiver & Event-Driven Ingestion** — GitHub, GitLab, Slack, Shortcut, generic handlers
- [ ] **2. Agent Sandbox Integration (gVisor / Warm Pools)** — kernel-level isolation, warm pool CRD
- [x] **3. OpenCode Execution Engine** — BYOM terminal-native agent
- [x] **4. Cline CLI Execution Engine** — headless CI/CD mode, MCP support
- [x] **5. Shortcut.com Ticketing Backend** — REST API v3 integration
- [x] **6. Telegram Notification Channel** — Bot API notifications
- [x] **7. Linear Ticketing Backend** — GraphQL API integration
- [x] **8. Discord Notification Channel** — webhook-based notifications
- [x] **9. HashiCorp Vault Secrets Backend** — K8s auth, KV v2
- [x] **10. Task-Scoped Secret Resolution** — multi-backend, alias system, audit trail
- [x] **11. NetworkPolicy & Security Hardening** — agent/controller NetworkPolicy, PDB
- [ ] **12. Plugin SDKs (Python, Go, TypeScript)** — generated from protobuf definitions
- [x] **13. Local Development Mode (Docker Compose)** — Docker execution backend

**Score: 12 / 13 complete**

---

## Strategic Roadmap

### Phase A — Foundation

#### 1. Enhanced Claude Code Engine: Structured Output, Tool Control, Model Fallback

**Priority:** Critical
**Scope:** Medium (4-6 files)
**Dependencies:** None

Extend the Claude Code engine with production-ready CLI flags for structured output, model fallback, tool whitelisting, and guard rail injection.

- [x] Add `--output-format stream-json` / `--json-schema` for type-safe structured `TaskResult` output
- [x] Add `--fallback-model` support (e.g. `haiku`) for automatic failover
- [x] Add `--no-session-persistence` for stateless container execution
- [x] Add `--append-system-prompt` for guard rail injection (separates guard rails from task prompt)
- [x] Add `--tools` / `--allowedTools` / `--disallowedTools` driven by task profile config
- [x] Add functional options: `WithFallbackModel`, `WithToolWhitelist`, `WithJSONSchema`
- [x] Extend `EngineConfig` with `FallbackModel`, `ToolWhitelist`, `JSONSchema` fields
- [x] Extend `ClaudeCodeConfig` YAML fields (`fallback_model`, `tool_whitelist`, `json_schema`)
- [x] Extend prompt builder task profiles to drive tool whitelist per task type
- [x] Extend table-driven tests in `pkg/engine/claudecode/engine_test.go`

---

#### 3. Engine Fallback Chains and Auto-Selection

**Priority:** High
**Scope:** Medium (5-7 files)
**Dependencies:** None (all 5 engines already exist)

If Claude Code fails, retry with Cline. If Cline rate-limits, try Aider. Transforms RoboDev from a single-engine orchestrator into a resilient execution platform.

- [x] Add `FallbackEngines []string` to `EnginesConfig`
- [x] Create `internal/controller/engine_selector.go` with `EngineSelector` interface
- [x] Implement default selection: ticket label override → `[default] + fallback_engines`
- [x] Modify `ProcessTicket` to store engine list on TaskRun
- [x] Modify job failure handler to retry with next engine before exhausting retries
- [x] Add `EngineAttempts []string` and `CurrentEngine string` to TaskRun
- [x] Write `tests/integration/engine_fallback_test.go`

---

#### 6. TDD-Driven Agent Workflow Mode

**Priority:** Medium
**Scope:** Small (2-3 files)
**Dependencies:** Item 1 (structured output)

Structure the agent's workflow: write failing test → implement → verify tests pass. Produces verifiably correct output.

- [x] Add `WorkflowMode` field to task profiles in prompt builder
- [x] Implement `tdd` workflow: inject structured test-first instructions
- [x] Implement `review-first` workflow mode
- [x] Add `Workflow string` to `TaskProfileConfig` (`""` | `"tdd"` | `"review-first"`)
- [x] Verify `tests_passed`, `tests_failed`, `tests_added` flow from JSON schema to watchdog

---

### Phase B — Streaming

#### 2. Real-Time Agent Streaming via stream-json

**Priority:** Critical
**Scope:** Large (8-10 files)
**Dependencies:** Item 1

Replace heartbeat polling with real-time NDJSON telemetry from Claude Code's `stream-json` output format. No other harness streams agent progress back to the control plane.

- [x] Create `internal/agentstream/events.go` — event types (`ToolCallEvent`, `ContentDeltaEvent`, `CostEvent`, `ResultEvent`)
- [x] Create `internal/agentstream/reader.go` — K8s pod log streaming, NDJSON parsing
- [x] Create `internal/agentstream/forwarder.go` — forward events to watchdog and notification channels
- [x] Update Claude Code engine to use `--output-format stream-json --verbose` when streaming enabled
- [x] Add `StreamingSource` input to watchdog alongside existing heartbeat source
- [x] Start stream reader goroutine per active Claude Code job in controller
- [x] Non-streaming engines (Codex, Aider, OpenCode, Cline) fall back to heartbeat mechanism
- [x] Add optional live progress forwarding to notification channels (`notifications.live_updates: true`)
- [x] Write unit tests for NDJSON parser and event types
- [x] Write integration tests for stream reader with mock pod logs

---

### Phase C — Isolation

#### 4. Agent Sandbox Integration (gVisor / Warm Pools)

**Priority:** High
**Scope:** Large (10+ files)
**Dependencies:** Cluster needs agent-sandbox controller installed

Native gVisor integration via `kubernetes-sigs/agent-sandbox` for kernel-level isolation and warm pools for sub-second cold starts.

- [x] Add `ExecutionConfig` with `Backend string` (`"job"` | `"sandbox"` | `"local"`) to config
- [x] Create `internal/sandboxbuilder/builder.go` — emits `Sandbox` CRs instead of `batch/v1.Job`
- [x] Implement `SandboxClaim` abstraction against alpha API changes
- [x] Add warm pool config per engine (different images need separate pools)
- [x] RuntimeClass defaults to gVisor, Kata as opt-in
- [x] Update controller to select builder based on `config.Execution.Backend`
- [x] Add `templates/runtimeclass-gvisor.yaml` to Helm chart (gated by `sandbox.enabled`)
- [x] Add `templates/sandboxwarmpool.yaml` to Helm chart (gated by `sandbox.warmPool.enabled`)
- [x] Add `sandbox` section to `values.yaml`
- [x] Add environment variable stripping to each engine entrypoint (read API keys into memory, delete from `os.environ`)
- [x] Write integration tests for sandbox builder (CRD generation only)

---

### Phase D — Governance

#### 7. Governance: Approval Workflows and Audit Trail

**Priority:** Medium
**Scope:** Medium (5-7 files)
**Dependencies:** Webhook server (already complete)

Wire the existing approval interface, add persistent TaskRun storage, and build approval gates for enterprise governance.

- [x] Wire approval backend in controller; add approval gate checks before job creation (`require_approval_before_start`)
- [x] Add approval gate check before marking complete (`require_approval_before_merge`)
- [x] Handle Slack interactive message callbacks to resolve pending approvals
- [x] Create `internal/taskrun/store.go` — `TaskRunStore` interface (`Save`, `Get`, `List`, `ListByTicketID`)
- [x] Implement in-memory store (default)
- [ ] Implement SQLite store (local mode)
- [ ] Implement PostgreSQL store (production)
- [x] Add `ApprovalGates []string` and `ApprovalCostThresholdUSD` to guard rails config
- [x] Add `TaskRunStore` config section (`backend`, `sqlite.path`, `postgres.*`)
- [ ] Extend secret resolver audit logging to write to TaskRunStore

---

### Phase E — Access

#### 8. Local Development Mode (Docker Compose)

**Priority:** Medium
**Scope:** Medium (5-7 files)
**Dependencies:** None

A `docker compose up` experience that dramatically lowers the barrier to adoption.

- [x] Create `internal/jobbuilder/docker.go` — `DockerBuilder` implementing `JobBuilder` using Docker API
- [x] Support `execution.backend: local` in config to trigger DockerBuilder
- [x] Select builder based on `execution.backend` in `cmd/robodev/main.go`
- [x] Create `docker-compose.yaml` — controller + webhook server
- [x] Extend noop ticketing backend with file-watcher mode (reads tasks from YAML file)
- [x] Add `compose-up` / `compose-down` Makefile targets
- [ ] Write quickstart guide for Docker Compose mode

---

#### 5. Multi-Agent Coordination Layer (Phase 1 — In-Process Teams)

**Priority:** High
**Scope:** Large (10+ files)
**Dependencies:** Items 1 and 4

Orchestrate cross-engine teams where Claude Code plans and Aider executes. Phase 1 covers in-process Claude Code teams only.

- [x] Support `--agents` flag in Claude Code engine when `AgentTeamsConfig.Enabled` is true
- [x] Populate `AgentTeamsConfig` with `Agents map[string]AgentDef` and `MaxTeammates int`
- [x] Generate agent definitions from task type in prompt builder (e.g. `coder` + `reviewer` for bug-fix)
- [x] Write integration tests for team-enabled engine spec generation

**Phase 2 (future — not tracked here):**
- Multi-pod teams via `internal/teamcoordinator/`
- Multi-job decomposition for compound tasks
- `ParentTaskRunID` for sub-task tracking
- Agent Relay SDK as communication sidecar

---

### Phase F — Ecosystem

#### 9. Plugin SDKs (Python, Go, TypeScript)

**Priority:** Medium
**Scope:** Large (separate repositories)
**Dependencies:** Protobuf definitions (already complete in `proto/`)

Generated SDKs so third-party plugin authors don't need to implement raw gRPC.

- [ ] Configure `buf.gen.yaml` for multi-language stub generation
- [ ] **Python SDK** (`unitaryai/robodev-plugin-sdk-python`)
  - [ ] Generate Python gRPC stubs
  - [ ] Build base classes with `scaffold`/`serve`/`test` CLI commands
  - [ ] Include example plugins (noop ticketing, file-based ticketing, webhook notification)
- [ ] **Go SDK** (`unitaryai/robodev-plugin-sdk-go`)
  - [ ] Generate Go gRPC stubs (separate module)
  - [ ] Build thin wrapper with hashicorp/go-plugin boilerplate
  - [ ] Include example plugins
- [ ] **TypeScript SDK** (`unitaryai/robodev-plugin-sdk-ts`)
  - [ ] Generate TypeScript gRPC stubs
  - [ ] Build base classes wrapping grpc-js
  - [ ] Include example plugins
- [ ] Publish SDK documentation to `docs/plugins/`

---

### Phase G — Observability

#### 10. Agent Dashboard (Web UI)

**Priority:** High
**Scope:** Large (new service)
**Dependencies:** Items 2 (streaming) and 7 (audit trail/TaskRun store)

A web dashboard for real-time agent observability and control. Shows live agent status, task run history, streaming progress, cost tracking, and provides manual controls (approve, cancel, retry).

**Approach options:**
- **Grafana-based** — Use existing Prometheus metrics + Loki logs + custom Grafana panels. Lowest effort, leverages existing metrics infrastructure. Limited interactivity (read-only, no approve/cancel actions).
- **Custom lightweight UI** — Go backend serving a React/Next.js frontend. Full control over UX, interactive approval buttons, live streaming via SSE/WebSocket. More effort but purpose-built for the use case.
- **Hybrid** — Grafana for metrics/dashboards + a thin custom control plane UI for interactive actions (approvals, cancellation, manual task submission). Best of both worlds.

**Minimum viable features:**
- [ ] Real-time TaskRun status view (queued, running, succeeded, failed, needs-human)
- [ ] Live streaming progress per agent (tool calls, token usage, cost) from agentstream events
- [ ] Task run history with filtering by engine, ticket, status, date range
- [ ] Cost tracking dashboard (per-task, per-engine, daily/weekly aggregates)
- [ ] Manual controls: approve pending tasks, cancel running tasks, retry failed tasks
- [ ] Engine health overview (which engines are available, fallback chain status)

**If custom UI:**
- [ ] Create `cmd/robodev-dashboard/` — separate binary or embed in controller
- [ ] Add `/api/v1/` REST endpoints: `GET /taskruns`, `GET /taskruns/:id`, `POST /taskruns/:id/approve`, `POST /taskruns/:id/cancel`, `GET /taskruns/:id/stream` (SSE)
- [ ] Frontend: React + Tailwind or similar lightweight stack
- [ ] WebSocket/SSE endpoint for live streaming events from agentstream

**If Grafana-based:**
- [ ] Create Grafana dashboard JSON provisioning in `charts/robodev/dashboards/`
- [ ] Add Loki log aggregation for structured slog output
- [ ] Custom Grafana panels for TaskRun state machine visualisation
- [ ] Alert rules for cost velocity, stalled agents, failed tasks

---

### Phase H — Documentation

#### 11. Documentation Site

**Priority:** High
**Scope:** Medium (new site, existing content)
**Dependencies:** None

A polished, searchable documentation site that makes RoboDev look production-grade. First impressions matter — a good docs site is the difference between "interesting project" and "I'm deploying this on Monday."

**Approach options:**
- **Docusaurus** — React-based, MDX support, versioning, search, dark mode out of the box. Used by most major OSS projects. Easy to deploy on Vercel/Netlify/GitHub Pages.
- **Astro Starlight** — Newer, faster, built for docs. Excellent DX, automatic sidebar from file structure, i18n support, lighter than Docusaurus.
- **MkDocs Material** — Python-based, gorgeous Material Design theme, built-in search, mermaid diagrams. Simpler than Docusaurus, very popular in the Go/K8s ecosystem.

**Site structure:**
- [ ] Landing page with hero, feature highlights, and quick start
- [ ] Getting Started guide (K8s deploy, Docker Compose local mode, first task)
- [ ] Architecture overview with diagrams (controller, engines, plugins, state machine)
- [ ] Configuration reference (full YAML schema with examples)
- [ ] Engine guides (Claude Code, Codex, Aider, OpenCode, Cline) with comparison matrix
- [ ] Plugin development guide (ticketing, notifications, secrets, approval, review, SCM)
- [ ] Security model documentation (threat model, gVisor, NetworkPolicy, guard rails)
- [ ] API reference (webhook endpoints, protobuf service definitions)
- [ ] Deployment guides (Helm chart reference, production hardening, multi-tenancy)
- [ ] Changelog and migration guides
- [ ] Search functionality
- [ ] Dark mode support

**Infrastructure:**
- [ ] Choose site framework (Docusaurus / Astro Starlight / MkDocs Material)
- [ ] Set up `docs/site/` or `website/` directory
- [ ] CI/CD pipeline for automatic deployment on merge to main
- [ ] Custom domain setup (docs.robodev.dev or similar)
- [ ] Add `make docs-serve` / `make docs-build` targets

---

## Implementation Order

```
Phase A (foundation):  1. Enhanced Claude Code Engine  ·  3. Engine Fallback Chains  ·  6. TDD Workflow Mode
Phase B (streaming):   2. Real-Time Agent Streaming
Phase C (isolation):   4. Agent Sandbox Integration
Phase D (governance):  7. Approval Workflows + Audit Trail
Phase E (access):      8. Local Development Mode  ·  5. Multi-Agent Coordination (Phase 1)
Phase F (ecosystem):   9. Plugin SDKs
Phase G (observe):    10. Agent Dashboard
Phase H (docs):       11. Documentation Site
```

---

## Summary

| # | Feature | Phase | Priority | Status |
|---|---------|-------|----------|--------|
| 1 | Enhanced Claude Code Engine | A | Critical | **Complete** |
| 2 | Real-Time Agent Streaming | B | Critical | **Complete** |
| 3 | Engine Fallback Chains | A | High | **Complete** |
| 4 | Agent Sandbox Integration | C | High | **Complete** |
| 5 | Multi-Agent Coordination (Phase 1) | E | High | **Complete** |
| 6 | TDD Workflow Mode | A | Medium | **Complete** |
| 7 | Approval Workflows + Audit Trail | D | Medium | **Complete** |
| 8 | Local Development Mode | E | Medium | **Complete** |
| 9 | Plugin SDKs | F | Medium | Not started |
| 10 | Agent Dashboard | G | High | Not started |
| 11 | Documentation Site | H | High | Not started |
