package domain

import (
	"time"

	"github.com/google/uuid"
)

type ExecutionSuspensionID struct{ Value uuid.UUID }

func NewExecutionSuspensionID() ExecutionSuspensionID {
	return ExecutionSuspensionID{Value: uuid.New()}
}

func (id ExecutionSuspensionID) String() string { return id.Value.String() }

type SuspensionReason string

const (
	SuspensionApproval SuspensionReason = "APPROVAL"
	SuspensionSubtask  SuspensionReason = "SUBTASK"
)

// ExecutionSuspension is the one generic mechanism a paused Execution's ExecutionJob (status
// AWAITING_RESUME, no lease) waits on — an ApprovalRequest being decided (Reason=APPROVAL,
// ResumeKey=the ApprovalRequestID) or a delegated sub-task's Task completing
// (Reason=SUBTASK, ResumeKey=the child TaskID). Whatever resolves the ResumeKey is the only
// thing allowed to flip the job back to PENDING — nothing else ever touches an AWAITING_RESUME
// job, which is exactly what keeps the sweeper from mistaking "waiting on a human" for "worker
// crashed mid-lease".
type ExecutionSuspension struct {
	ID          ExecutionSuspensionID
	ExecutionID ExecutionID
	Reason      SuspensionReason
	ResumeKey   string
	CreatedAt   time.Time
	ResolvedAt  *time.Time
}

func NewExecutionSuspension(executionID ExecutionID, reason SuspensionReason, resumeKey string) ExecutionSuspension {
	return ExecutionSuspension{
		ID: NewExecutionSuspensionID(), ExecutionID: executionID, Reason: reason,
		ResumeKey: resumeKey, CreatedAt: time.Now().UTC(),
	}
}

func (s ExecutionSuspension) Resolved() ExecutionSuspension {
	now := time.Now().UTC()
	s.ResolvedAt = &now
	return s
}
