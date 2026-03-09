package local

import (
	"time"

	"github.com/unitaryai/robodev/pkg/engine"
	"github.com/unitaryai/robodev/pkg/plugin/ticketing"
)

// State represents the persisted lifecycle state for a local ticket.
type State string

const (
	StateReady      State = "ready"
	StateInProgress State = "in_progress"
	StateCompleted  State = "completed"
	StateFailed     State = "failed"
)

const (
	stateReady      = StateReady
	stateInProgress = StateInProgress
	stateCompleted  = StateCompleted
	stateFailed     = StateFailed
)

func (s State) isTerminal() bool {
	return s == stateCompleted || s == stateFailed
}

// CommentKind identifies the source of a persisted comment.
type CommentKind string

const (
	CommentKindSystem CommentKind = "system"
	CommentKindUser   CommentKind = "user"
)

const (
	commentKindSystem = CommentKindSystem
	commentKindUser   = CommentKindUser
)

// EventType identifies a lifecycle or audit event stored for a ticket.
type EventType string

const (
	eventCommentAdded     EventType = "comment_added"
	eventCreated          EventType = "created"
	eventImported         EventType = "imported"
	eventMarkedComplete   EventType = "marked_complete"
	eventMarkedFailed     EventType = "marked_failed"
	eventMarkedInProgress EventType = "marked_in_progress"
	eventRequeued         EventType = "requeued"
)

// StoredTicket is the admin/read model exposed by the local backend.
type StoredTicket struct {
	Ticket        ticketing.Ticket   `json:"ticket"`
	State         State              `json:"state"`
	FailureReason string             `json:"failure_reason"`
	Result        *engine.TaskResult `json:"result,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
	InProgressAt  *time.Time         `json:"in_progress_at,omitempty"`
	CompletedAt   *time.Time         `json:"completed_at,omitempty"`
	FailedAt      *time.Time         `json:"failed_at,omitempty"`
}

// Ticket converts the record to the controller-facing ticket shape.
func (r StoredTicket) TicketRecord() ticketing.Ticket {
	return r.Ticket
}

// StoredComment is the persisted representation of a ticket comment.
type StoredComment struct {
	ID        int64       `json:"id"`
	TicketID  string      `json:"ticket_id"`
	Kind      CommentKind `json:"kind"`
	Body      string      `json:"body"`
	CreatedAt time.Time   `json:"created_at"`
}
