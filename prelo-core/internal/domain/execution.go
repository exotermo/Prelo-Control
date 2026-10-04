package domain

import "time"

type ExecutionStatus string

const (
	ExecutionPending   ExecutionStatus = "PENDING"
	ExecutionRunning   ExecutionStatus = "RUNNING"
	ExecutionCompleted ExecutionStatus = "COMPLETED"
	ExecutionFailed    ExecutionStatus = "FAILED"
)

// Execution transitions are unconditional (no guard), matching the Java record — concurrency
// safety comes from the Task/job optimistic lock, not from Execution's own state machine.
type Execution struct {
	ID                ExecutionID
	TaskID            TaskID
	AgentID           AgentID
	Status            ExecutionStatus
	StartedAt         *time.Time
	CompletedAt       *time.Time
	Error             *string
	Result            *string
	RequestID         *string
	Model             *string
	Provider          *string
	AgentVersion      *string
	ContextSnapshotID *ContextSnapshotID
	Version           int64
}

func NewPendingExecution(taskID TaskID, agentID AgentID) Execution {
	return Execution{
		ID:      NewExecutionID(),
		TaskID:  taskID,
		AgentID: agentID,
		Status:  ExecutionPending,
	}
}

func (e Execution) Running() Execution {
	now := time.Now().UTC()
	e.Status = ExecutionRunning
	e.StartedAt = &now
	e.CompletedAt = nil
	e.Error = nil
	return e
}

// Completed sets the result along with the model/provider actually used by the Gateway
// (post-fallback) and the requestId that was sent.
func (e Execution) Completed(result, requestID, model, provider string) Execution {
	now := time.Now().UTC()
	e.Status = ExecutionCompleted
	e.CompletedAt = &now
	e.Error = nil
	e.Result = &result
	e.RequestID = &requestID
	e.Model = &model
	e.Provider = &provider
	return e
}

func (e Execution) Failed(message string) Execution {
	now := time.Now().UTC()
	e.Status = ExecutionFailed
	e.CompletedAt = &now
	e.Error = &message
	return e
}

func (e Execution) WithVersion(newVersion int64) Execution {
	e.Version = newVersion
	return e
}

func (e Execution) WithContextSnapshotID(id ContextSnapshotID) Execution {
	e.ContextSnapshotID = &id
	return e
}

func (e Execution) WithAgentVersion(agentVersion string) Execution {
	e.AgentVersion = &agentVersion
	return e
}

func (e Execution) WithRequestID(requestID string) Execution {
	e.RequestID = &requestID
	return e
}
