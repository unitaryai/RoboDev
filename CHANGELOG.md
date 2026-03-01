# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### Strategic Roadmap Phase A-E (Items 1-8)

##### Enhanced Claude Code Engine (Item 1)
- `DefaultTaskResultSchema` const with JSON schema for structured task result output
- Functional options pattern: `WithFallbackModel`, `WithToolWhitelist`, `WithJSONSchema`
- Conditional CLI flag construction: `--output-format stream-json`, `--json-schema`, `--fallback-model`, `--no-session-persistence`, `--append-system-prompt`, `--allowedTools`, `--disallowedTools`
- Extended `EngineConfig` with `FallbackModel`, `ToolWhitelist`, `ToolBlacklist`, `JSONSchema`, `AppendSystemPrompt`, `NoSessionPersistence`, `StreamingEnabled` fields
- Extended `ClaudeCodeEngineConfig` with matching YAML fields

##### Real-Time Agent Streaming (Item 2)
- New `internal/agentstream/` package: NDJSON event types (`ToolCallEvent`, `ContentDeltaEvent`, `CostEvent`, `ResultEvent`), K8s pod log stream reader, event forwarder to watchdog and notifications
- Watchdog `ConsumeStreamEvent` method for live tool call tracking, cost monitoring, and heartbeat updates from streaming agents
- Controller stream reader lifecycle: starts per active Claude Code job, cancels on completion/failure
- `StreamingConfig` with `Enabled` and `LiveNotifications` fields
- `--verbose` flag added when streaming is enabled for richer event data

##### Engine Fallback Chains (Item 3)
- `EngineSelector` interface with `DefaultEngineSelector` implementation
- Ticket label override (`robodev:engine:<name>`) for per-ticket engine selection
- `FallbackEngines []string` in `EnginesConfig` for ordered fallback chain
- `EngineAttempts` and `CurrentEngine` tracking on TaskRun
- Automatic fallback to next engine in `handleJobFailed` before exhausting retries
- `launchFallbackJob` helper with unique job IDs per attempt

##### Agent Sandbox Integration (Item 4)
- `SandboxBuilder` implementing `JobBuilder` with gVisor/Kata RuntimeClass, SandboxClaim annotations, and warm pool labels
- `ExecutionConfig` with `Backend` ("job"/"sandbox"/"local") and `SandboxConfig` (runtime class, warm pool, env stripping)
- Helm templates: `runtimeclass-gvisor.yaml` and `sandboxwarmpool.yaml` (gated by `sandbox.enabled`)
- Environment variable stripping in Claude Code entrypoint (guarded by `ENV_STRIPPING`)

##### Multi-Agent Coordination Phase 1 (Item 5)
- `AgentDef` config type with role, model, and instructions
- `BuildAgentFlags` generating `--agents` JSON from config or task-type defaults (bug_fix: coder+reviewer, feature: coder+reviewer+tester)
- `WithTeamsConfig` functional option on Claude Code engine
- Team coordination prompt section in `BuildPromptWithTeams`
- `CLAUDE_CODE_MAX_TEAMMATES` environment variable

##### TDD Workflow Mode (Item 6)
- `WorkflowInstructions()` function returning structured instructions for "tdd" and "review-first" modes
- `TaskProfileConfig` with `Workflow`, `ToolWhitelist`, `ToolBlacklist` fields
- Workflow instructions injected between task description and guard rails in prompt template
- `BuildPromptWithProfile` method for profile-aware prompt construction

##### Approval Workflows and Audit Trail (Item 7)
- `TaskRunStore` interface with `Save`, `Get`, `List`, `ListByTicketID` methods
- `MemoryStore` thread-safe in-memory implementation
- Pre-start and pre-merge approval gates in controller
- Queued → NeedsHuman state transition for approval holds
- Slack approval callback parsing (`robodev_approve_*` / `robodev_reject_*` actions)
- `ApprovalGates` and `ApprovalCostThresholdUSD` in guard rails config

#### Local Development Mode (Docker Compose)
- DockerBuilder (`internal/jobbuilder/docker.go`) implementing `controller.JobBuilder` for local Docker execution — produces K8s Job objects annotated with `robodev.io/execution-backend: local`
- Builder selection in `cmd/robodev/main.go`: reads `execution.backend` config to choose between standard ("job"), sandbox, or local Docker builder
- Noop ticketing file-watcher mode: `NewWithTaskFile` constructor reads tasks from a local YAML file, enabling local development without a real ticketing provider
- `FileTask` struct for YAML task definitions with ID, title, description, repo URL, and labels
- `docker-compose.yaml` for running the controller outside Kubernetes with config and workspace volume mounts
- Makefile targets: `compose-up` and `compose-down` for Docker Compose lifecycle
- Table-driven tests for DockerBuilder (backend annotation, security context, volumes, env vars, resources)
- Tests for noop file-watcher (valid YAML, empty file, malformed YAML, missing file, in-progress filtering)

#### Governance: Approval Workflows, Audit Trail, and TaskRun Store
- TaskRunStore interface (`internal/taskrun/store.go`) with `Save`, `Get`, `List`, and `ListByTicketID` methods for persistent TaskRun storage
- MemoryStore in-memory implementation of TaskRunStore with thread-safe operations
- Approval gates configuration: `approval_gates` list and `approval_cost_threshold_usd` in GuardRailsConfig (values: "pre_start", "high_cost", "pre_merge")
- Pre-start approval gate: when "pre_start" is configured, TaskRun transitions Queued → NeedsHuman instead of launching a K8s Job, requiring human approval before execution begins
- Pre-merge approval gate: when "pre_merge" is configured, completed jobs transition Running → NeedsHuman instead of marking the ticket complete, requiring human approval before merge
- TaskRunStoreConfig with backend selection ("memory", "sqlite", "postgres") and SQLite path configuration — future durable backends can implement the TaskRunStore interface
- Reconciler now persists TaskRun state to the store after every state transition (creation, approval hold, running, completion, failure, retry)
- `WithTaskRunStore` reconciler option; defaults to MemoryStore when not provided
- Slack webhook handler now parses `robodev_approve_*` and `robodev_reject_*` action IDs from approval callbacks, extracting task run IDs and logging structured approval/rejection events (stub for future resolution wiring)
- Queued → NeedsHuman added as a valid state transition in the TaskRun state machine
- Table-driven tests for MemoryStore (save/get, list, filter by ticket ID, not-found error)
- Controller tests for pre-start approval gate blocking job creation, pre-merge gate holding completion, and store persistence verification
- Config loading tests for new governance fields (approval gates, cost threshold, taskrun store backend)

#### Integration Test Suite
- Comprehensive three-tier integration test architecture: Tier 1 (E2E against Kind cluster), Tier 2 (in-process with fake K8s client), Tier 3 (in-process, no K8s)
- No-op ticketing backend (`pkg/plugin/ticketing/noop/`) as fallback for webhook-only and test deployments — prevents nil-pointer panics when no ticketing backend is configured
- Tier 3 tests: engine spec validation across all 5 engines, JobBuilder security hardening verification
- Tier 2 tests: full reconciler pipeline, webhook-to-reconciler integration, TaskRun state machine lifecycle, guard rails with real reconciler wiring, secret resolver parsing and policy enforcement
- Tier 1 E2E tests: webhook signature validation (GitHub HMAC-SHA256), container security contexts, Helm resource verification, NetworkPolicy rules, Prometheus metrics endpoints
- Test orchestration script (`hack/run-integration-tests.sh`) producing markdown reports suitable for `claude -p` analysis
- Test Helm values overlay (`hack/values-test.yaml`) with webhook secrets and NetworkPolicy enabled
- Makefile targets: `deploy-test`, `integration-test`, `test-report`, `test-all`

#### Webhook Receiver & Event-Driven Ingestion
- HTTP webhook server (`internal/webhook/`) with route handlers for GitHub, GitLab, Slack, Shortcut, and a configurable generic handler
- GitHub HMAC-SHA256 signature validation (`X-Hub-Signature-256`), issue event parsing for `opened` and `labeled` actions
- GitLab secret token validation (`X-Gitlab-Token`), issue and merge request event parsing
- Slack signing secret validation (`X-Slack-Signature`) with 5-minute replay attack prevention
- Shortcut webhook handler with optional HMAC signature validation and story state change parsing
- Generic webhook handler with configurable HMAC or bearer token auth and dot-notation JSON field mapping
- Webhook server wired into `cmd/robodev/main.go` with graceful shutdown support
- New `WebhookConfig` in controller configuration for per-source secrets

#### Task-Scoped Secret Resolution
- Secret resolver (`internal/secretresolver/`) parsing `<!-- robodev:secrets -->` HTML comment blocks and `robodev:secret:` label prefixes
- Policy engine validating environment variable names against allowed/blocked glob patterns, URI scheme restrictions, and tenant scoping
- Multi-backend resolver dispatching secrets by URI scheme (`vault://`, `k8s://`, `alias://`) to registered backends
- Structured audit logging (secret names only, never values) for compliance and debugging
- New `SecretResolverConfig` and `VaultSecretsConfig` types in controller configuration

#### HashiCorp Vault Secrets Backend
- Vault backend (`pkg/plugin/secrets/vault/`) implementing `secrets.Backend` interface
- Kubernetes auth method: reads ServiceAccount token and authenticates to Vault's `/v1/auth/kubernetes/login` endpoint
- KV v2 secret reads with token caching for performance
- Uses `net/http` directly — no external Vault client library dependency

#### OpenCode Execution Engine
- OpenCode engine (`pkg/engine/opencode/`) implementing `ExecutionEngine` for the OpenCode CLI
- Command: `opencode --non-interactive --message <prompt>`, context file: `AGENTS.md`
- Supports Anthropic, OpenAI, and Google model providers
- OpenCode engine Dockerfile (`docker/engine-opencode/`) with `node:22-slim` base, OpenCode CLI, and non-root user
- OpenCode entrypoint script with repo cloning, multi-provider support, and semantic exit codes
- Makefile targets: `docker-build-engine-opencode`, `docker-build-dev-engine-opencode`

#### Cline Execution Engine
- Cline engine (`pkg/engine/cline/`) implementing `ExecutionEngine` for the Cline CLI
- Command: `cline --headless --task <prompt> --output-format json`, context file: `.clinerules`
- Supports Anthropic, OpenAI, Google, and AWS Bedrock model providers
- Optional MCP (Model Context Protocol) support via `WithMCPEnabled` option and `--mcp` flag
- Cline engine Dockerfile (`docker/engine-cline/`) with `node:22-slim` base, headless CLI, and non-root user
- Cline entrypoint script with repo cloning, MCP toggle (`MCP_ENABLED`), JSON output, and semantic exit codes
- Makefile targets: `docker-build-engine-cline`, `docker-build-dev-engine-cline`

#### Shortcut Ticketing Backend
- Shortcut.com backend (`pkg/plugin/ticketing/shortcut/`) implementing `ticketing.Backend`
- REST API v3 integration with story search, label management, comments, and completion
- Auth via `Shortcut-Token` header, configurable workflow state ID and label filtering

#### Linear Ticketing Backend
- Linear backend (`pkg/plugin/ticketing/linear/`) implementing `ticketing.Backend`
- GraphQL API integration with issue queries, state transitions, comments, and label management
- Auth via raw API key, configurable team ID, state filter, and label filtering

#### Telegram Notification Channel
- Telegram channel (`pkg/plugin/notifications/telegram/`) implementing `notifications.Channel`
- Bot API `sendMessage` endpoint with Markdown formatting and optional topic-based thread support
- Builder options: `WithAPIURL`, `WithHTTPClient`, `WithThreadID`

#### Discord Notification Channel
- Discord channel (`pkg/plugin/notifications/discord/`) implementing `notifications.Channel`
- Webhook-based with colour-coded rich embeds (green success, red failure, blue info)
- No auth library needed — webhook URL contains the token

#### NetworkPolicy & Security Hardening
- Agent NetworkPolicy (`networkpolicy-agent.yaml`): deny all ingress, egress allowed to DNS (53), HTTPS (443), SSH (22) only
- Controller NetworkPolicy (`networkpolicy-controller.yaml`): allow webhook and metrics ingress, egress to DNS, HTTPS, and K8s API
- PodDisruptionBudget (`pdb.yaml`): configurable `minAvailable` / `maxUnavailable`, defaults to `minAvailable: 1`
- All templates gated by `networkPolicy.enabled` and `pdb.enabled` values
- Webhook Service template (`service-webhook.yaml`) exposing webhook port separately from metrics
- Webhook Ingress template (`ingress.yaml`) with className, annotations, hosts, paths, and TLS support
- Webhook container port added to controller Deployment template (conditional on `webhook.enabled`)
- New Helm values: `webhook` (with `service`, `ingress` sub-config), `networkPolicy`, `pdb` sections

#### GitHub Backend Filtering
- GitHub ticketing backend now supports filtering by assignee, milestone, and issue state in addition to labels
- Added client-side label exclusion to prevent re-pickup of in-progress and failed issues (default: `in-progress`, `robodev-failed`)
- New functional options: `WithAssignee`, `WithMilestone`, `WithState`, `WithExcludeLabels`
- Labels filter is now optional — omitting it enables assignee-only or milestone-only workflows
- Refactored `PollReadyTickets` URL construction to use `url.Values` for safer query parameter encoding

#### Live End-to-End Testing
- Wired up `cmd/robodev/main.go` with full backend initialisation: K8s client, GitHub ticketing, Claude Code engine, job builder, and Slack notifications
- Controller now reads secrets from Kubernetes at startup (GitHub token, Slack token) using config-driven secret references
- Added `hack/setup-secrets.sh` interactive script for provisioning K8s secrets (GitHub token, Anthropic API key, Slack bot token)
- Added `hack/values-live.yaml` Helm values overlay for live testing with real backends (conservative guardrails: $10 cost cap, 30min timeout, max 2 jobs)
- Added Makefile targets: `setup-secrets`, `live-deploy`, `live-up`, `live-redeploy` for full live testing workflow
- In-cluster and kubeconfig fallback for K8s client creation (supports both deployed and local dev)

#### Local Development & E2E Testing
- Kind cluster configuration (`hack/kind-config.yaml`) with two-node topology and host port mapping
- Local dev Helm values overlay (`hack/values-dev.yaml`) with `pullPolicy: Never`, disabled leader election, and NodePort access
- End-to-end smoke test suite (`tests/e2e/`) covering deployment readiness, health endpoints, metrics, RBAC, and resource creation
- Makefile targets for full local workflow: `check-prereqs`, `kind-create`, `kind-delete`, `docker-build-dev`, `kind-load`, `deploy`, `undeploy`, `local-up`, `local-down`, `local-redeploy`, `e2e-test`, `logs`
- Configurable service type in Helm chart (supports NodePort for local development)
- Local development workflow documentation in CONTRIBUTING.md

#### Phase 1: Core Framework & Abstractions
- Protobuf definitions for all 7 plugin interfaces: ticketing, notifications, approval, secrets, review, SCM, engine (plus shared common.proto)
- buf.yaml and buf.gen.yaml for protobuf linting and Go/gRPC code generation
- Makefile targets: `proto-lint` and `proto-gen` for protobuf workflow
- All plugin Go interfaces: ticketing.Backend, notifications.Channel, approval.Backend, secrets.Backend, review.Backend, scm.Backend
- gRPC plugin host (pkg/plugin/host.go) using hashicorp/go-plugin with version handshake, crash detection, and restart with exponential backoff
- JobBuilder (internal/jobbuilder) translating ExecutionSpec to K8s batch/v1.Job with security contexts, tolerations, and labels
- Progress watchdog (internal/watchdog) with anomaly detection rules: loop detection, thrashing, stall, cost velocity, telemetry failure
- Cost tracker (internal/costtracker) with per-engine token rates and budget checking
- Prompt builder (internal/promptbuilder) with guard rails injection and task profile support
- Controller reconciliation loop with ticket polling, idempotency, guard rails validation, job lifecycle management, and retry logic
- Health endpoints (/healthz, /readyz) and Prometheus metrics serving in main.go
- Graceful shutdown with signal handling (SIGTERM, SIGINT)

#### Phase 2: Claude Code Engine + GitHub + Slack
- Claude Code execution engine (pkg/engine/claudecode) with BuildExecutionSpec, BuildPrompt, and hooks generation
- Claude Code hooks system for guard rails: PreToolUse blockers, PostToolUse heartbeat, Stop handler
- GitHub Issues ticketing backend (pkg/plugin/ticketing/github) with REST API integration
- GitHub SCM backend (pkg/plugin/scm/github) for branch and pull request management
- Kubernetes Secrets backend (pkg/plugin/secrets/k8s) with secret retrieval and env var building
- Slack notification channel (pkg/plugin/notifications/slack) with Block Kit formatted messages
- Slack approval backend (pkg/plugin/approval/slack) with interactive messages and callback handling

#### Phase 2D: Dockerfiles & Helm Chart
- Multi-stage controller Dockerfile (golang:1.23-alpine builder to distroless runtime)
- Claude Code engine Dockerfile with Node.js, Claude Code CLI, git, gh, python3, guard rail hooks
- Claude Code entrypoint.sh with repo cloning, CLAUDE.md injection, and semantic exit codes
- Guard rail hooks: block-dangerous-commands.sh (PreToolUse/Bash) and block-sensitive-files.sh (PreToolUse/Write|Edit)
- OpenAI Codex engine Dockerfile with Node.js, Codex CLI, and full-auto entrypoint
- Helm chart: plugin init container support via values.yaml plugins array and shared emptyDir volume
- Helm chart: metrics Service, leader election flag, and post-install NOTES.txt
- Makefile targets: docker-build-controller, docker-build-engine-claude-code, docker-build-engine-codex, docker-build, helm-lint
- .dockerignore for clean container builds

#### Phase 3: Codex + Aider + GitLab + CodeRabbit
- OpenAI Codex execution engine (pkg/engine/codex) with prompt-based guard rails
- Aider execution engine (pkg/engine/aider) with conventions.md support
- Engine registry (pkg/engine/registry.go) for engine selection and management
- GitLab SCM backend (pkg/plugin/scm/gitlab) with merge request support
- CodeRabbit review backend (pkg/plugin/review/coderabbit) for quality gate integration

#### Phase 4: Agent Teams, Scaling & Multi-Tenancy
- Claude Code agent teams configuration (pkg/engine/claudecode/teams.go) with in-process mode
- Multi-tenancy support via namespace-per-tenant model in config
- Quality gate configuration with security checks
- Progress watchdog configuration in main config
- Extended engine configs with auth settings (API key, Bedrock, Vertex, setup-token)
- Karpenter NodePool example (examples/karpenter/) with spot instances and taints
- KEDA ScaledObject example (examples/keda/) for Prometheus-based scaling
- Example configurations: github-slack, gitlab-teams, enterprise (with full feature set)
- Example third-party plugins: Jira (Python) and Microsoft Teams (TypeScript)
- Grafana dashboard JSON (charts/robodev/dashboards/)
- Scaling documentation (docs/scaling.md)

#### Phase 5: Community & Documentation
- Comprehensive README with architecture diagram, quick start, and full feature overview
- Guard rails documentation (docs/guardrails.md) covering all six layers
- Plugin interface documentation: ticketing, notifications, secrets, engines
- Expanded CI pipeline: protobuf linting, Helm linting, Docker build verification
- Release workflow with cosign image signing and syft SBOM generation
- GitHub issue templates (bug report, feature request, plugin request)
- Pull request template with checklist
- Comprehensive architecture documentation with system diagrams and component details
- Full security documentation with threat model, defence in depth, and container hardening
- Getting started guide with step-by-step quick start, configuration reference, and troubleshooting
- Plugin development guide covering Go built-in and gRPC third-party plugin authoring
- Expanded plugin interface docs: ticketing (269 lines), notifications (246 lines), secrets (261 lines), engines (500 lines) with full RPC coverage, implementation guidance, and design considerations

#### Infrastructure
- Go module and core skeleton: controller entrypoint, config loading, TaskRun state machine, Prometheus metrics, ExecutionEngine interface
- CI pipeline with lint, test, build, proto-lint, helm-lint, docker-build jobs
- Helm chart with deployment, RBAC, ConfigMap, ServiceMonitor, Service templates
- Community files: CONTRIBUTING.md, CODE_OF_CONDUCT.md, SECURITY.md
- Comprehensive table-driven tests for all packages (20 test suites)
