// Package usecase defines the data-driven "use case" model described in
// docs/designs/use-case-abstraction.md: a Definition descriptor that
// captures the execution mode, gate configuration, and result handling
// that distinguish one dispatch pipeline shape (ticketing, incident
// triage, and future consumers) from another.
//
// This package currently implements only the subset of the design doc's
// section 4 model needed to register descriptors and tag TaskRuns with
// their use case: ExecutionMode, Gates, the ResultHandler interface, and
// a minimal Definition holding Name/ExecutionMode/Gates/Results. It is
// intentionally inert: nothing in the controller consumes a Definition's
// Gates or Results yet, and ResultHandler has no implementation. The
// function-valued hooks from the design doc's fuller Definition shape
// (IdempotencyKey, TaskRunID, BuildTask, ConfigureEngine) are not present
// here; they migrate into this package in a later PR alongside the
// dispatch changes that will actually call them. Until then,
// ProcessTicket and ProcessIncidentEvent keep building idempotency keys,
// TaskRun IDs, and engine.Task values themselves.
package usecase

import (
	"context"

	"github.com/unitaryai/osmia/internal/taskrun"
	"github.com/unitaryai/osmia/pkg/engine"
)

// ExecutionMode names the shape of a use case's execution: whether it
// clones a repository and opens a merge request, works read-only against
// existing state, or only calls SCM/ticketing APIs. See section 5 of
// docs/designs/use-case-abstraction.md for the full taxonomy. The
// engine.Task field that lets this vary per task instance, rather than
// per use case, is not added in this PR; today the mode lives only on
// Definition.
type ExecutionMode string

const (
	// ModeClonePushMR clones a repository, branches, commits, pushes, and
	// opens a merge request. This is today's ticketing default.
	ModeClonePushMR ExecutionMode = "clone_push_mr"
	// ModeReadOnly works against a live checkout or read-only mirror, or
	// does not touch a repository filesystem at all, without opening a
	// merge request.
	ModeReadOnly ExecutionMode = "read_only"
	// ModeAPIRead has no workspace and no clone: the agent only calls
	// SCM/ticketing APIs. This is incident triage's mode today.
	ModeAPIRead ExecutionMode = "api_read"
)

// Gates is a boolean set describing which optional pipeline steps a use
// case opts into. Every field defaults to false, so a new use case is
// gate-free unless it explicitly opts in.
type Gates struct {
	// ApprovalGates enables the pre-start/pre-merge human approval gates.
	ApprovalGates bool
	// CostEstimation enables predictive cost estimation and auto-rejection
	// of tasks whose estimate exceeds the configured budget.
	CostEstimation bool
	// EngineSelector enables the configured EngineSelector fallback chain,
	// rather than a single fixed engine.
	EngineSelector bool
	// GuardRails enables guard-rail validation before launch.
	GuardRails bool
	// MarkInProgress enables the ticketing.MarkInProgress call once a job
	// launches.
	MarkInProgress bool
	// MemoryQuery enables the episodic memory query that enriches the
	// prompt before launch.
	MemoryQuery bool
	// NotifyStart enables runNotifyStart / thread-ref injection.
	NotifyStart bool
	// RepoURLRequired enables repository-URL resolution (description
	// extraction, then interactive polling) before launch.
	RepoURLRequired bool
	// Tournament enables the competitive tournament coordinator when
	// enough engines are registered.
	Tournament bool
}

// ResultHandler dispatches completion and failure outcomes for a use
// case's TaskRuns. It is defined here as the target shape for the
// result-handler taxonomy in section 6 of
// docs/designs/use-case-abstraction.md (open_mr, notify_only,
// comment_and_notify), but is not implemented or consumed by any
// Definition in this PR. handleJobComplete and handleJobFailed continue
// to call ticketing.MarkComplete/MarkFailed unconditionally, exactly as
// before; wiring a concrete ResultHandler per use case is the next PR.
type ResultHandler interface {
	// OnSuccess runs when a use case's TaskRun completes successfully.
	OnSuccess(ctx context.Context, tr *taskrun.TaskRun, result *engine.TaskResult) error
	// OnFailure runs when a use case's TaskRun fails.
	OnFailure(ctx context.Context, tr *taskrun.TaskRun, reason string) error
}

// Definition describes one dispatch pipeline shape. It is a data-driven
// descriptor, not a Process() entry point: a future PR's shared
// controller pipeline will call hooks derived from it in a fixed order.
// This PR keeps Definition to the fields it actually uses today (Name,
// ExecutionMode, Gates, and the as-yet-unconsumed Results), rather than
// the fuller shape (with function-valued hooks) described in the design
// doc; see the package doc for what migrates here later.
type Definition struct {
	// Name is the use case's canonical identifier, used as the registry
	// key and as the value persisted in TaskRun.UseCase.
	Name string
	// ExecutionMode is the execution-mode taxonomy value for this use
	// case (see ExecutionMode).
	ExecutionMode ExecutionMode
	// Gates records which optional pipeline steps this use case opts
	// into.
	Gates Gates
	// Results is the result handler for this use case's TaskRuns. Not
	// yet consumed by any dispatch code; see the ResultHandler doc
	// comment.
	Results ResultHandler
}
