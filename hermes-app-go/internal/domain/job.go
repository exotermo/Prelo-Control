package domain

import "time"

// JobStatus is the durable execution queue's own state machine (etapa 6.5), separate from
// Task/Execution status. PENDING/RETRY are claimable; CLAIMED/RUNNING are held by a worker;
// DONE/FAILED are normal terminal outcomes (mirroring the Execution's own COMPLETED/FAILED);
// DEAD is a manual/ops terminal state for a job that exhausted MaxAttempts. AWAITING_RESUME
// (etapa loop de ferramentas/orquestrador) is neither claimable nor terminal — it holds no
// lease, and only the resolution of its ExecutionSuspension is allowed to move it back to
// PENDING; the Sweeper must never treat it as an orphaned lease (there is no lease to expire).
type JobStatus string

const (
	JobPending        JobStatus = "PENDING"
	JobClaimed        JobStatus = "CLAIMED"
	JobRunning        JobStatus = "RUNNING"
	JobDone           JobStatus = "DONE"
	JobFailed         JobStatus = "FAILED"
	JobRetry          JobStatus = "RETRY"
	JobDead           JobStatus = "DEAD"
	JobAwaitingResume JobStatus = "AWAITING_RESUME"
)

// ExecutionJob is the queue/scheduling record for a given Execution — kept separate from
// task_executions (which stays the audit/result record) so the queue table can be
// reasoned about, and later archived, independently. ID and ExecutionID carry the same UUID
// on purpose: the Execution's id is the idempotency key across the whole pipeline (also sent
// to the Gateway as requestId), and the job row just adds scheduling metadata on top of it.
type ExecutionJob struct {
	ID             ExecutionID
	TaskID         TaskID
	ExecutionID    ExecutionID
	Status         JobStatus
	Priority       int16
	Attempt        int
	MaxAttempts    int
	AvailableAt    time.Time
	ClaimedBy      *string
	ClaimedAt      *time.Time
	LeaseExpiresAt *time.Time
	LastError      *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int64
}

func NewExecutionJob(taskID TaskID, executionID ExecutionID) ExecutionJob {
	now := time.Now().UTC()
	return ExecutionJob{
		ID:          executionID,
		TaskID:      taskID,
		ExecutionID: executionID,
		Status:      JobPending,
		MaxAttempts: 5,
		AvailableAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
