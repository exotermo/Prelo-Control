package domain

import "fmt"

// InvalidTransitionError mirrors the Java InvalidTaskTransitionException: raised when a
// Task or Execution status change is attempted from a status that doesn't allow it.
type InvalidTransitionError struct {
	Entity string
	From   string
	To     string
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("cannot transition %s from %s to %s", e.Entity, e.From, e.To)
}

// ErrUnknownAgent mirrors UnknownAgentException.
type ErrUnknownAgent struct {
	AgentID string
}

func (e *ErrUnknownAgent) Error() string {
	return fmt.Sprintf("unknown agent: %s", e.AgentID)
}

// ValidationError mirrors the IllegalArgumentException checks done in the Java domain
// record compact constructors.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}
