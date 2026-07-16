# ADR 0002: Managed Agents self-hosted sandbox interop

## Status

Proposed. This records the outcome of a research spike: a timeboxed prototype is
approved, there is no product commitment.

## Context

Anthropic's Managed Agents platform lets a customer keep orchestration (the model,
session state, and the work queue) on Anthropic's side while running the tool
execution itself on infrastructure the customer controls. This is exposed through a
`self_hosted` environment type. We evaluated whether this interoperates with Osmia's
existing architecture, and whether adopting it would add anything Osmia does not
already have.

All evidence below was gathered from Anthropic's official documentation on
2026-07-16:

- <https://platform.claude.com/docs/en/managed-agents/self-hosted-sandboxes>
- <https://platform.claude.com/docs/en/managed-agents/self-hosted-sandboxes-security>

### How self-hosted sandboxes work

A `self_hosted` environment acts as a work queue. When a session is assigned to it,
Anthropic enqueues the session as a work item. An "environment worker" (a process the
customer runs) claims work items, downloads the agent's skills to
`<workdir>/skills/<name>/`, executes tool calls locally against the standard toolset
(bash, read, write, edit, glob, grep), and posts results back. Tool inputs and
outputs flow through Anthropic's control plane so the model can decide what to do
next, but the filesystem, spawned processes, and network egress all stay entirely
local to the worker.

There are two worker patterns:

- **Always-on poller** (`ant beta:worker poll`): a long-running process that only
  needs outbound HTTPS.
- **Webhook-triggered sandbox-per-session**: a `session.status_run_started` webhook
  fires, the customer spawns an execution context (Anthropic's docs show a
  `docker run` example), and the SDK worker's handle-item call processes that one
  session and exits.

Pre-built workers ship in the CLI and in the Python, TypeScript, and Go SDKs
(`EnvironmentWorker`, with `HandleItem` in Go). Customers who need more control can
call the Environments Work endpoints directly and implement their own worker.

Authentication is an environment-scoped service key (`ANTHROPIC_ENVIRONMENT_KEY` plus
`ANTHROPIC_ENVIRONMENT_ID`), not a full API key. It authorises only claiming that
environment's work and posting results back for it.

Anthropic mounts no files or repositories itself: session `metadata` carries
references (for example an S3 path or a commit SHA), and the customer's spawn script
is responsible for staging files before execution. Workers can also serve custom
tools (SDK worker only) and wrap internal MCP servers as custom tools, so the MCP
server itself needs no inbound connectivity.

### Security model

The security model is an explicit shared responsibility split. The customer owns:

- sandbox image hardening (Anthropic explicitly recommends non-root, read-only root
  filesystem, and dropped capabilities)
- network egress restriction
- key storage and rotation (via a secrets manager)
- separation of environments across trust boundaries
- the blast radius of any tool the worker executes
- retention of logs and session content

Anthropic states plainly that it cannot instantly invalidate a leaked environment
key, cannot verify the customer's sandbox image, and that its security boundary
stops at the sandbox.

## Decision

**Go ahead with a timeboxed prototype. No product commitment.**

The rationale:

1. **Shape match is strong.** The webhook-triggered sandbox-per-session pattern maps
   nearly one-to-one onto Osmia's existing architecture. Osmia already has a webhook
   receiver, a jobbuilder that launches one hardened Kubernetes Job per task, and
   per-task secret injection. A "Managed Agents" flow would look like: MA webhook →
   a new use-case descriptor (the in-progress use-case abstraction makes this a
   registration, not a fork) → a Job whose container runs the Go SDK's
   `EnvironmentWorker.HandleItem` for that session → the Job exits when the session
   ends. The presumed mismatch between a long-lived session and a one-shot Job
   largely dissolves once you notice a Job can simply live for the duration of the
   session.
2. **Osmia's security posture already implements Anthropic's hardening
   recommendations.** `runAsNonRoot`, `readOnlyRootFilesystem`, dropping all
   capabilities, and NetworkPolicy-based egress control are Osmia's existing
   defaults, not new work. Most of the shared-responsibility list Anthropic hands
   the customer is already satisfied.
3. **The intelligence layer survives, locally.** Because the worker executes every
   tool call inside the pod, Osmia can wrap the SDK toolset to emit its existing
   NDJSON telemetry envelopes (tool_use, cost, result) from the worker, feeding the
   PRM, watchdog, cost tracking, and transcript sink exactly as for native engines.
   This is the differentiated angle: supervising Managed Agents sessions with
   Osmia's own reward model, on Osmia's own infrastructure, rather than ceding that
   supervision to a black-box managed runtime.
4. **Strategic hedge.** This positions Osmia as complementary to Anthropic's
   platform rather than competing with it, and it derisks the scenario where
   Managed Agents commoditises self-hosted harnesses over time.

### Prototype scope

- One new engine image: a Go worker wrapping the SDK toolset with telemetry
  emission.
- One use-case descriptor for Managed Agents sessions.
- Environment key delivered via existing secret plumbing (no new secret mechanism).
- Skills staging that honours the session's `metadata` (the customer-supplied
  references Anthropic itself does not resolve).

Success criterion: a Managed Agents session executes on an Osmia-launched Job with
its tool telemetry visible in Osmia's transcript and watchdog.

## Consequences

- No changes to the controller, jobbuilder, or any other Go package are made by this
  ADR. It records a decision to prototype; implementation is out of scope here.
- If the prototype succeeds, a follow-up ADR (or an update to this one) should record
  the design of the engine image, the use-case descriptor, and any changes needed to
  the secret resolver or telemetry pipeline.
- If the prototype fails or the unknowns below prove blocking, this ADR should be
  superseded with a rejection recorded and the reason documented.

## Open questions

These are unknowns the prototype is expected to resolve, not blockers to starting
it:

- Maturity of the Go SDK worker: this is a beta surface and its stability is
  unverified.
- Work-queue claim semantics under concurrent workers, and whether retries are
  at-least-once.
- Startup-latency expectations for webhook-triggered spawns. This is undocumented:
  it is not yet known whether a Kubernetes Job cold start is acceptable within
  Anthropic's expected response window.
- Pricing interaction: whether the published $0.08 per session-hour managed-agents
  fee applies unchanged when execution itself happens on self-hosted infrastructure.
- How a Managed Agents session identity maps to an Osmia TaskRun, and what
  idempotency guarantees are needed.
- How session interrupt or cancellation propagates to the worker.
- Whether tool-result size limits constrain large file reads.

## Rejected alternatives

- **Do nothing.** Misses both the strategic hedge and the local-supervision
  differentiator described above.
- **Full product commitment now.** Premature given the open questions above and
  given that the underlying API is still beta.
- **Always-on poller deployment instead of per-session Jobs.** A weaker fit for
  Osmia: an idle poller per environment loses Osmia's per-task isolation and its
  per-session hardware accounting, both of which the Job-per-session pattern
  preserves.

## References

- Anthropic, "Self-hosted sandboxes",
  <https://platform.claude.com/docs/en/managed-agents/self-hosted-sandboxes>
  (read 2026-07-16)
- Anthropic, "Self-hosted sandboxes: security",
  <https://platform.claude.com/docs/en/managed-agents/self-hosted-sandboxes-security>
  (read 2026-07-16)
