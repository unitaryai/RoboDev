# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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

#### Infrastructure
- Go module and core skeleton: controller entrypoint, config loading, TaskRun state machine, Prometheus metrics, ExecutionEngine interface
- CI pipeline with lint, test, and build jobs
- Helm chart skeleton with deployment, RBAC, ConfigMap, and ServiceMonitor templates
- Documentation stubs: getting started, architecture, plugin guide, security
- Community files: CONTRIBUTING.md, CODE_OF_CONDUCT.md, SECURITY.md
- Comprehensive table-driven tests for all packages (14 test suites)
