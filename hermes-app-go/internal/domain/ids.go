package domain

import "github.com/google/uuid"

type TaskID struct{ Value uuid.UUID }

func NewTaskID() TaskID { return TaskID{Value: uuid.New()} }

func (id TaskID) String() string { return id.Value.String() }

type ExecutionID struct{ Value uuid.UUID }

func NewExecutionID() ExecutionID { return ExecutionID{Value: uuid.New()} }

func (id ExecutionID) String() string { return id.Value.String() }

type ContextSnapshotID struct{ Value uuid.UUID }

func NewContextSnapshotID() ContextSnapshotID { return ContextSnapshotID{Value: uuid.New()} }

func (id ContextSnapshotID) String() string { return id.Value.String() }

// AgentID mirrors AgentId — a required, non-blank string, not a UUID.
type AgentID struct{ Value string }

func NewAgentID(value string) (AgentID, error) {
	if isBlank(value) {
		return AgentID{}, &ValidationError{Message: "agent id is required"}
	}
	return AgentID{Value: value}, nil
}

func (id AgentID) String() string { return id.Value }
