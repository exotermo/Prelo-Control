package domain

import "time"

type TaskStatus string

const (
	TaskCreated   TaskStatus = "CREATED"
	TaskQueued    TaskStatus = "QUEUED"
	TaskRunning   TaskStatus = "RUNNING"
	TaskCompleted TaskStatus = "COMPLETED"
	TaskFailed    TaskStatus = "FAILED"
)

// MaxDescriptionLength matches the `tasks.description VARCHAR(4000)` column (V2 migration).
const MaxDescriptionLength = 4000

// MaxDelegationDepth bounds how many levels of delegate_to_agent chains are allowed (Fase C) —
// a top-level Task has Depth 0; each delegation adds 1. Enforced in NewSubtask itself, not by
// the tool or the loop, so there is exactly one place this rule can ever be bypassed from.
const MaxDelegationDepth = 3

var taskRunnableFrom = map[TaskStatus]bool{TaskCreated: true, TaskQueued: true}
var taskTerminalFrom = map[TaskStatus]bool{TaskRunning: true}

type Task struct {
	ID TaskID
	// TenantID is populated for tasks created through the authenticated API. Legacy/internal
	// tasks may be empty while they are migrated; API lookups never expose those rows to a tenant.
	TenantID     string
	Description  string
	Status       TaskStatus
	CreatedAt    time.Time
	AgentID      AgentID
	Version      int64
	ParentTaskID *TaskID
	Depth        int
}

func NewTask(description string, agentID AgentID) (Task, error) {
	if isBlank(description) {
		return Task{}, &ValidationError{Message: "task description is required"}
	}
	if len(description) > MaxDescriptionLength {
		return Task{}, &ValidationError{Message: "task description must be at most 4000 characters"}
	}
	return Task{
		ID:          NewTaskID(),
		Description: description,
		Status:      TaskCreated,
		CreatedAt:   time.Now().UTC(),
		AgentID:     agentID,
		Version:     0,
	}, nil
}

// NewSubtask is the only way a Task ends up with a ParentTaskID — created by the
// delegate_to_agent tool (Fase C), never directly from a request. Depth is derived from the
// parent's own Depth, and MaxDelegationDepth is enforced right here: a delegation chain cannot
// be extended by constructing a Task some other way.
func NewSubtask(description string, agentID AgentID, parent Task) (Task, error) {
	if parent.Depth+1 > MaxDelegationDepth {
		return Task{}, &ValidationError{Message: "delegation depth exceeds the maximum allowed"}
	}
	task, err := NewTask(description, agentID)
	if err != nil {
		return Task{}, err
	}
	parentID := parent.ID
	task.ParentTaskID = &parentID
	task.Depth = parent.Depth + 1
	task.TenantID = parent.TenantID
	return task, nil
}

// Queued moves a task from CREATED into QUEUED, marking that an execution job has been
// enqueued for it. Only allowed from CREATED — a task already QUEUED cannot be re-enqueued,
// which is what makes a second concurrent POST .../execute on the same task naturally rejected.
func (t Task) Queued() (Task, error) {
	if t.Status != TaskCreated {
		return Task{}, &InvalidTransitionError{Entity: "task", From: string(t.Status), To: string(TaskQueued)}
	}
	t.Status = TaskQueued
	return t, nil
}

func (t Task) Running() (Task, error) {
	if !taskRunnableFrom[t.Status] {
		return Task{}, &InvalidTransitionError{Entity: "task", From: string(t.Status), To: string(TaskRunning)}
	}
	t.Status = TaskRunning
	return t, nil
}

func (t Task) Completed() (Task, error) {
	if !taskTerminalFrom[t.Status] {
		return Task{}, &InvalidTransitionError{Entity: "task", From: string(t.Status), To: string(TaskCompleted)}
	}
	t.Status = TaskCompleted
	return t, nil
}

func (t Task) Failed() (Task, error) {
	if !taskTerminalFrom[t.Status] {
		return Task{}, &InvalidTransitionError{Entity: "task", From: string(t.Status), To: string(TaskFailed)}
	}
	t.Status = TaskFailed
	return t, nil
}

// WithVersion reflects the row version handed back by the store after a successful write;
// callers should not construct this by hand.
func (t Task) WithVersion(newVersion int64) Task {
	t.Version = newVersion
	return t
}
