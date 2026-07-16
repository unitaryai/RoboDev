# Design: Use-Case Abstraction and Shadow Mode

Status: proposed, no code changes yet.
Owner: controller team.
Related: [roadmap item 24](../roadmap.md#24-non-standard-task-types-analysis-reporting-review).

## 1. Context and motivation

Osmia currently has one fully-built dispatch pipeline, `ProcessTicket`, and one
purpose-built parallel pipeline, `ProcessIncidentEvent`. The package comment at
the top of `internal/controller/incident.go:3-11` states the relationship
plainly:

> `ProcessIncidentEvent` is a reconciler entry point that runs in parallel to
> `ProcessTicket` rather than sharing code with it. ... A future refactor will
> lift both `ProcessTicket` and `ProcessIncidentEvent` behind a common
> interface; until then, the duplication is bounded and intentional.

That comment was written with intent: incident triage was shipped as a second,
narrower pipeline rather than as a generalisation of the first, because
generalising a state machine from a single example tends to produce the wrong
abstraction. Now that a second consumer shape exists (incident triage: no
repository, no merge request, single engine, no approval gates) and roadmap
item 24 describes a third (read-only analysis/reporting tasks with no git
clone at all and a comment-and-notify result), the codebase has enough
evidence to design the abstraction properly instead of guessing at it.

Roadmap item 24 ("Non-Standard Task Types (Analysis, Reporting, Review)",
`docs/roadmap.md:230-256`) already lists the open design questions this
document answers: execution-mode taxonomy, result-handler taxonomy, profile
dispatch, and prompt design. This document is the design doc that item asks
for. It does not implement the abstraction; it specifies what "use case"
means as a concept in Osmia, what the do-not-break contract is for the two
existing consumers, and how a third consumer would be added without another
bespoke `ProcessXEvent` function.

Future consumers this abstraction should accommodate, based on the
roadmap-24 sketch and the incident-triage precedent: read-only analysis
tasks (`api_read` or `read_only` execution mode, `comment_and_notify` result
handling), and any future webhook-driven flow that, like incident triage,
has no ticketing backend of its own.

## 2. Do-not-break contract

Any refactor must preserve every one of the following behaviours exactly.
Each is cited with its current file and line so a reviewer can re-check it
against the diff.

| Behaviour | Evidence |
|---|---|
| Ticketing idempotency key is `"<ticket.ID>-1"` | `internal/controller/controller.go:508` |
| Incident idempotency key is `"<incident.ID>:<event.Type>"` | `internal/controller/incident.go:79` |
| Ticketing TaskRun ID format `tr-<ticketID>-<unixMilli>` | `internal/controller/controller.go:614` |
| Incident TaskRun ID format `tr-incident-<lower(incidentID)>-<eventSuffix>-<unixMilli>` | `internal/controller/incident.go:119-123` |
| Empty `RepoURL` produces a workspace-only prompt with no git instructions | `pkg/engine/claudecode/engine.go:563-566` |
| Non-empty `RepoURL` produces the full clone/branch/push/MR prompt, further branching on `SessionID`, `PriorBranchName`, `PriorMergeRequestURL` | `pkg/engine/claudecode/engine.go:423-562` |
| `startStreamReader` only runs when `engineName == "claude-code"` | `internal/controller/controller.go:773` (ticketing), `internal/controller/incident.go:230-232` (incident, via `defaultIncidentEngine` constant) |
| Pre-start approval gate (`hasApprovalGate("pre_start")`) exists for ticketing, is skipped entirely for incident triage | `internal/controller/controller.go:633-666`; no equivalent call in `incident.go` |
| Pre-merge approval gate (`hasApprovalGate("pre_merge")`) exists for ticketing's `handleJobComplete`, applies today to incident runs too because they share `handleJobComplete` | `internal/controller/controller.go:908-945` |
| Episodic memory query and `runNotifyStart` run for ticketing only | `internal/controller/controller.go:670-687` (memory), `controller.go:708` (`runNotifyStart`); `incident.go:64-70` documents both as skipped |
| `ticketing.MarkInProgress` runs for ticketing only | `internal/controller/controller.go:765` |
| Per-flow Slack config: `IncidentTriage.SlackChannelID` / `SlackTokenSecret`, falling back to the first `Notifications.Channels` entry when empty | `internal/controller/incident.go:147-171`, config fields at `internal/config/config.go:199-216` |
| Per-flow incident.io MCP credentials: `IncidentTriage.IncidentIOAPIKeySecret` sets `SecretKeyRefs["INCIDENT_IO_API_KEY"]`, which `setup-claude.sh` reads to register the MCP server | `internal/controller/incident.go:172-185`; `internal/config/config.go:218-224`; `docker/engine-claude-code/setup-claude.sh:54-55` |
| Webhook route `/webhooks/incident-io`, Svix signature verification | `internal/webhook/server.go:177`; `internal/webhook/incident.go:202` (`verifySvixSignature`); `IncidentIOWebhookConfig` at `internal/config/config.go:174-181` |
| Known incident-UUID wart: `handleJobComplete` calls `ticketing.MarkComplete(ctx, tr.TicketID, result)` with the incident ID as `TicketID`; the ticketing backend does not recognise it, logs a non-fatal error | `internal/controller/controller.go:1044-1050`; documented at `internal/controller/incident.go:72-77` |
| Same wart on the failure path: `handleJobFailed` calls `ticketing.MarkFailed(ctx, tr.TicketID, reason)` | `internal/controller/controller.go:1533-1539` |

The last two rows are warts, not features, but "do not break" still applies
in the narrow sense that today they degrade gracefully (a logged error, not a
crash). Section 6 proposes fixing them as part of this refactor rather than
preserving them, because a use-case-aware result handler makes the fix
nearly free and the current behaviour produces noisy, confusing logs on
every single incident run.

## 3. Decision: descriptor with hooks, not an interface, not pure config

Three shapes were considered.

**Option A: fat `UseCase` interface.** Each use case implements a Go interface
with a `Process(ctx, event) error` method that owns its entire pipeline,
mirroring how `ProcessTicket` and `ProcessIncidentEvent` work today. This is
the least disruptive option but does not solve the actual problem: it
formalises "one bespoke function per use case" instead of removing the
duplication. A third consumer under this option is still a full rewrite of
job launch, idempotency, gate checks, and completion handling.

**Option B: pure YAML/config-driven use cases.** Execution mode, result
handler, gates, and prompt shape are all declared in `osmia-config.yaml` with
no per-use-case Go code. This was rejected because several of the
distinguishing behaviours are not declarative: how the idempotency key is
built, how the TaskRun ID is formatted (DNS-1123 constraints differ per
source system), which fields populate `engine.Task`, and how Slack fallback
resolves are all pieces of logic, not scalar config values. Forcing this
logic into a config DSL would recreate a worse version of Go inside YAML.

**Option C (chosen): a data-driven `Definition` descriptor with hook
functions, plus a `ResultHandler` interface, in a new `internal/usecase`
package.** A use case is a struct of small, mostly-pure functions (build the
idempotency key, build the TaskRun ID, build the `engine.Task`, decide which
gates apply) registered by name. The shared pipeline in the controller loops
over data (which gates apply, which execution mode, which result handler)
rather than branching on `if engineName == "claude-code"`-style ad hoc
checks scattered through two files. New use cases are additions to a
registry map, not new top-level `ProcessX` functions.

The test this design must pass: **adding a third consumer (the roadmap-24
read-only analysis flow) should require one new `Definition` value and one
new config block, not a new file that duplicates `ProcessTicket`'s
plumbing.** Section 4 defines the `Definition` shape that makes this true.

## 4. Use-case model

```go
package usecase

// Definition describes one dispatch pipeline shape. It is a set of hook
// functions and declarative flags, not a Process() entry point: the
// shared controller pipeline calls these hooks in a fixed order, so a
// new use case cannot silently skip a step the way a hand-written
// ProcessX function could.
type Definition struct {
    Name          string
    ExecutionMode ExecutionMode // see section 5
    Gates         Gates
    Results       ResultHandler // see section 6
    Shadow        ShadowConfig  // see section 7

    IdempotencyKey func(event any) string
    TaskRunID      func(event any) string
    BuildTask      func(event any) (engine.Task, error)
    ConfigureEngine func(cfg *engine.EngineConfig, event any)
}

// Gates is a boolean set. Every field defaults to false so a new use
// case is gate-free (matching incident triage's current behaviour)
// unless it opts in explicitly.
type Gates struct {
    PreStart          bool // hold in NeedsHuman before launch
    PreMerge          bool // hold in NeedsHuman before marking complete
    CodeReview        bool // run the configured review backend
    EpisodicMemory    bool // query memory before building the prompt
    NotifyStart       bool // runNotifyStart / thread-ref injection
    MarkInProgress    bool // ticketing.MarkInProgress call
}
```

Gate table for the two existing consumers, derived from section 2's
citations:

| Gate | Ticketing | Incident triage |
|---|---|---|
| `PreStart` | true (`controller.go:633`) | false |
| `PreMerge` | true (`controller.go:908`, currently applies to incident runs too as a side effect of sharing `handleJobComplete`, see section 6) | false today, becomes explicitly false under the refactor |
| `CodeReview` | true if `config.CodeReview.Enabled` (`controller.go:950`) | false |
| `EpisodicMemory` | true (`controller.go:670-687`) | false |
| `NotifyStart` | true (`controller.go:708`) | false |
| `MarkInProgress` | true (`controller.go:765`) | false |

The registry is a `map[string]*Definition` keyed by name (for example
`"ticketing"`, `"incident_triage"`). A new field, `TaskRun.UseCase string`,
is persisted alongside the existing `TaskRun` fields
(`internal/taskrun/taskrun.go:56-` onward has no such field today) so that
`handleJobComplete` and `handleJobFailed` can look up the right
`Definition` for an in-flight or resumed TaskRun without re-deriving it from
the TaskRun ID's shape.

Until that field exists on every persisted TaskRun, the `tr-incident-`
prefix on the TaskRun ID (`internal/controller/incident.go:119`) is the only
signal available for inferring use case on old records. The refactor should
add a small legacy-inference shim, "TaskRun IDs starting with `tr-incident-`
without a persisted `UseCase` field are the incident_triage use case,
everything else is ticketing", and that shim should be removed at a release
boundary once no unresolved TaskRuns from before the migration remain (see
section 8).

Scope note: this design covers the launch tail and completion/failure
dispatch. `ProcessTicket`'s front half, repo-URL resolution, Slack polling,
engine selection, cost estimation, and tournament dispatch
(`controller.go:530-610`), is out of scope for v1 and stays as-is; incident
triage does not use any of it today, and folding it into the shared
descriptor is a separate, larger piece of work that is not required to
satisfy roadmap item 24.

## 5. Execution-mode taxonomy

Roadmap item 24 already names the three modes needed:

- `clone_push_mr`: today's default. Clone, branch, commit, push, open an MR.
- `read_only`: no git clone; the agent works against a live checkout or
  read-only mirror, or does not touch a repository filesystem at all.
- `api_read`: no workspace, no clone; the agent only calls SCM/ticketing
  APIs (for example, "list open MRs needing review").

This should become an explicit field, `ExecutionMode` on `Definition` and
(where a task can vary per-instance rather than per-use-case) an
`engine.Task.ExecutionMode` field, rather than being inferred solely from
`RepoURL` presence as it is today.

The existing inference must be preserved exactly, because it is what every
engine's `BuildPrompt` currently branches on:

- `RepoURL == ""` produces the plain "work in /workspace, write
  result.json" prompt with no git instructions
  (`pkg/engine/claudecode/engine.go:563-566`).
- `RepoURL != ""` produces the full clone/branch/push/MR flow, further
  branching on `SessionID` (resumed session skips clone,
  `engine.go:498-515`), `PriorBranchName` (recovery clone of a prior
  branch, `engine.go:432-453`), and `PriorMergeRequestURL` (push to
  existing MR instead of opening a new one, `engine.go:485-494`).

The recommended approach is an `effectiveMode()` helper: if
`Definition.ExecutionMode` is unset, fall back to the empty-`RepoURL`
convention indefinitely, so this is additive rather than a breaking change
to every engine's `BuildPrompt`. Concretely:

```go
func effectiveMode(task engine.Task, def *Definition) ExecutionMode {
    if def.ExecutionMode != "" {
        return def.ExecutionMode
    }
    if task.RepoURL == "" {
        return ModeAPIRead // or ModeReadOnly, see open questions
    }
    return ModeClonePushMR
}
```

Each engine's `BuildPrompt` and `BuildExecutionSpec` would eventually branch
on `effectiveMode()` instead of `task.RepoURL != ""` directly, but the
inferred default for existing callers does not change, so this can land
without touching every engine implementation in the same PR. This taxonomy
should be coordinated with any parallel engine-parity work on the prompt
contract (for example Codex/Aider `BuildPrompt` equivalents), since
`read_only` and `api_read` prompts need equivalent treatment in every
engine, not only claude-code.

## 6. Result-handler taxonomy

Three handlers, one already implicit, one fixing existing warts, one new:

- **`open_mr`** (today's implicit ticketing behaviour): on success, call
  `ticketing.MarkComplete`; on failure, call `ticketing.MarkFailed`; both
  keyed on `tr.TicketID`, which is a real ticket ID for this use case.
- **`notify_only`**: on success or failure, skip `ticketing.MarkComplete` /
  `MarkFailed` entirely and only run the notification path
  (`r.notifiers`, `updateNotificationStatus`). This directly fixes both
  warts in section 2: incident triage's `TicketID` is an incident UUID that
  no ticketing backend recognises, and today's code calls
  `MarkComplete`/`MarkFailed` on it anyway
  (`controller.go:1044-1050`, `controller.go:1533-1539`), producing a
  logged-but-ignored error on every single incident run. Under
  `notify_only`, incident triage's result handler simply does not call
  either ticketing method, so the error disappears rather than being
  suppressed after the fact.
- **`comment_and_notify`** (new, per roadmap item 24): on success, post the
  agent's summary as a ticket comment (distinct from `MarkComplete`, which
  transitions ticket state) and notify configured channels; no MR is
  expected. This is the handler a read-only analysis use case would use.

Dispatch becomes a lookup on `Definition.Results` (an interface with
`OnSuccess(ctx, tr, result) error` and `OnFailure(ctx, tr, reason) error`)
inside `handleJobComplete` and `handleJobFailed`, replacing the current
unconditional `r.ticketing.MarkComplete` / `MarkFailed` calls at
`controller.go:1044-1050` and `controller.go:1533-1539`.

**Behaviour tightening to flag explicitly:** today, because
`handleJobComplete` is shared code with no use-case awareness, incident
runs pass through the exact same pre-merge approval gate check as ticketing
runs (`controller.go:908`, `hasApprovalGate("pre_merge")`). In practice this
gate is never held for incident runs today only because operators do not
configure a `pre_merge` gate on deployments that also run incident triage,
not because the code prevents it. Once `Gates.PreMerge` is explicit per
`Definition` (section 4) and incident triage's `Definition` sets it to
`false`, an incident run can no longer be accidentally held at a pre-merge
approval gate, even if an operator later enables that gate for the
ticketing flow. This is a deliberate behaviour change, not an oversight: it
formalises what is currently true only by convention, but it means an
operator who was relying on the shared code path to gate incident runs
(unlikely, since no config exposes that combination as intentional) would
need to add an explicit incident-triage gate config once one exists.

## 7. Shadow mode

Shadow mode lets a use case run end to end, including calling out to
external systems for read purposes, while suppressing any write/mutating
side effect (ticket state changes, MR creation, Slack "task complete"
posts) so that a new use case or a risky prompt change can be validated in
production traffic without user-visible consequences.

**Config surface.** A global `shadow` block sets the default; each use
case's config block, for example `incident_triage.shadow`, can override it;
a task-profile-level flag can override both for a specific task type.
Default is off everywhere. This mirrors the existing per-flow override
pattern already used for `IncidentTriage.Engine` and
`IncidentTriage.AppendSystemPrompt` (`internal/config/config.go:188-225`).

**Four enforcement layers**, from strongest to weakest:

1. **Controller-enforced result-handler suppression (hard).** When
   `TaskRun.Shadow` is true, the `Definition.Results` dispatch in section 6
   is bypassed entirely in favour of a shadow-only handler that logs the
   would-be action and posts to a dedicated shadow-feedback channel
   instead of calling `ticketing.MarkComplete`/`MarkFailed`, posting a
   real Slack completion message, or registering an MR with the review
   poller. This layer also suppresses `recordTaskOutcome` and
   `extractMemory` (`controller.go:1073`, `controller.go:1093`), because
   shadow runs should not influence engine calibration or episodic memory
   built from real production outcomes.
2. **Prompt preamble plus `ProposedActions` (soft).** The prompt gains a
   preamble telling the agent it is running in shadow mode: it should
   still do its normal analysis and decide what it would do, but describe
   the action rather than performing it where practical (for example,
   describe the MR it would open rather than opening one). `TaskResult`
   gains a `ProposedActions []string` field for the agent to report this
   explicitly. This layer is soft because it depends on the agent
   following the instruction; nothing in the harness verifies compliance.
3. **In-pod PreToolUse guard hook (nominally hard, honestly mostly
   inert).** The plan for this layer is a `PreToolUse` hook that blocks
   mutating tool calls (git push, SCM write calls) when `OSMIA_SHADOW=1`
   is set in the pod environment, merged into
   `GenerateHooksConfig`'s existing `Bash`/`Write|Edit` matcher blocks
   (`pkg/engine/claudecode/hooks.go:71-133`). Three things must be stated
   honestly about this layer, because they materially weaken it:
   - `GenerateHooksConfig` has no production caller today; the only
     caller in the repository is its own test,
     `pkg/engine/claudecode/engine_test.go:1243`. The generated hooks
     JSON is never written into a running pod's settings.
   - `docker/engine-claude-code/settings.json` ships only a `permissions`
     block; it has no `hooks` key at all today.
   - The scripts a hooks config would reference
     (`/opt/osmia/hooks/heartbeat.sh`, `/opt/osmia/hooks/on-complete.sh`)
     do not exist on disk; only `block-dangerous-commands.sh` and
     `block-sensitive-files.sh` exist, in
     `docker/engine-claude-code/hooks/`.
   - Even once wired, a `Bash`/`Write|Edit` matcher hook cannot intercept
     MCP tool calls (for example a hypothetical ticket-comment MCP tool),
     because MCP tool invocations are a different tool-call shape and do
     not match those matchers. So this layer, even at full strength, only
     covers shell/file-write mutation, not MCP-mediated mutation.

   Given this, layer 3 should be described in any shadow-mode
   implementation plan as a gap to close, not a control that already
   exists. Layer 1 (controller-enforced suppression) is the layer that
   actually protects production state today and should be treated as the
   real guarantee; layers 2 and 3 are best-effort/defence-in-depth once
   built.
4. **Credential minimisation (documented, not enforced).** Operators
   running shadow-mode use cases are advised to configure a read-only or
   scoped-down SCM/ticketing token for the shadow deployment, since the
   harness itself does not currently enforce least-privilege on a
   per-shadow-run basis. This is a deployment-time recommendation, not a
   code-level control.

**Annotations and observability.** A `TaskRun.Shadow bool` field, a Job
label `osmia.io/shadow: "true"`, and a new Prometheus counter
`osmia_shadow_task_runs_total` (labelled by use case and outcome) give
operators a way to find and count shadow runs distinctly from real ones.

**Feedback loop, v1 scope.** For the first version, feedback from shadow
runs is a structured post to a dedicated shadow-feedback channel (what the
agent proposed, whether it succeeded, cost, duration), for a human to read
and compare against expectations. Automated comparison against a golden
set, or promotion of a shadow use case to live status, is out of scope for
v1 and is not designed here.

## 8. Migration and sequencing

The downstream ArgoCD deployment tracks `main` continuously (see project
memory: the `0.0.0-edge` Helm chart is republished on every push to `main`
and pulled automatically). This means every merged PR in this refactor must
leave the system in a fully working, individually deployable state; there
is no batched "big bang" release to hide behind.

Recommended sequencing:

1. **Contract tests first.** Write tests that pin every row in section 2's
   do-not-break table (idempotency key formats, TaskRun ID formats, prompt
   branching on `RepoURL`/`SessionID`/`PriorBranchName`, gate applicability
   per flow, the two `MarkComplete`/`MarkFailed` call sites) against
   today's code, before any refactor lands. These tests are the safety net
   for every subsequent step.
2. **Strangler refactor**, one behaviour at a time, each landing as its own
   deployable PR: introduce the `internal/usecase` package and
   `Definition` type without changing any call site; wire
   `ProcessIncidentEvent` to build its `Definition` and route through the
   new gate/result-handler dispatch (this is the PR that fixes the
   `MarkComplete`/`MarkFailed` warts, since incident triage is the
   simpler of the two flows to convert first); leave `ProcessTicket`'s
   front half untouched per section 4's scope note and only route its
   completion/failure tail through the same dispatch that
   `ProcessIncidentEvent` now uses.
3. **Release boundary before removing the `tr-incident-` legacy shim**
   (section 4). The shim can only be safely deleted once every
   in-flight/resumable TaskRun created before the `TaskRun.UseCase` field
   existed has reached a terminal state, which in practice means picking a
   release/version boundary and documenting it (analogous to the
   `NoSessionPersistence` field removal precedent already in this
   codebase) rather than deleting it opportunistically.

## 9. Open questions

- **Mutating incident.io MCP tool deny-list naming.** Once incident triage
  gains any mutating incident.io MCP tools (for example, updating incident
  status), shadow mode's layer 3 (section 7) needs a name-based deny-list
  for those specific MCP tool names, since matcher-based hooks cannot see
  them. What that deny-list configuration surface looks like (per-tool
  allow-list versus deny-list, where it is declared) is not decided here.
- **Read-only token strategy for clone-mode shadow.** If a shadow-mode use
  case still uses `clone_push_mr` execution mode (to validate a prompt
  change end to end before trusting it with real MRs), what credential
  should it clone with so that an agent cannot push even if layer 1 or 2
  fails? A read-only deploy token scoped per shadow run is one option; the
  operational cost of provisioning those tokens is not assessed here.
- **Whether to fix the hooks-wiring gap as part of this work.** Section
  7's honest note is that `GenerateHooksConfig` is currently dead code with
  no caller, no corresponding `hooks` key in `settings.json`, and missing
  target scripts. Should wiring it up (calling it from job/pod setup,
  writing `heartbeat.sh`/`on-complete.sh`, adding the `hooks` key to
  `settings.json`) be done as a prerequisite PR in this same sequencing, or
  tracked as a separate, independent piece of work? This document takes no
  position; it only records that shadow mode's layer 3 depends on that gap
  being closed to be more than aspirational.
