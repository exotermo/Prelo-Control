package domain

import (
	"strconv"
	"time"

	"github.com/google/uuid"
)

type ExecutionTurnID struct{ Value uuid.UUID }

func NewExecutionTurnID() ExecutionTurnID { return ExecutionTurnID{Value: uuid.New()} }

func (id ExecutionTurnID) String() string { return id.Value.String() }

type TurnKind string

const (
	TurnLLMCall  TurnKind = "LLM_CALL"
	TurnToolCall TurnKind = "TOOL_CALL"
	TurnSubtask  TurnKind = "SUBTASK"
)

// ExecutionTurn is the append-only ledger of everything that happened inside one Execution's
// agent loop — every Gateway call, every tool call, every sub-task delegation becomes exactly
// one row, in order. This is the single source of truth the loop replays from on resume (after
// an approval or a sub-task completes) and what the observability API/UI reads to render a full
// trace — one ledger, two consumers, never two sources of truth.
//
// RequestID is deliberately deterministic — TurnRequestID(executionID, turnNumber) — never a
// fresh UUID per attempt: if a worker crashes and the sweeper reprocesses the job, recomputing
// the same turn re-derives the same RequestID, so a retried Gateway/tool call for a turn that
// already completed is recognizable as a repeat, not a new side effect. This extends the same
// idempotency pattern the single-call path already used (execution.ID as the one and only
// requestId) to a per-turn granularity.
type ExecutionTurn struct {
	ID          ExecutionTurnID
	ExecutionID ExecutionID
	TurnNumber  int
	Kind        TurnKind
	RequestID   string
	Input       string
	Output      *string
	Error       *string
	StartedAt   time.Time
	CompletedAt *time.Time
}

// TurnRequestID is the deterministic idempotency key for one turn — see ExecutionTurn's doc.
func TurnRequestID(executionID ExecutionID, turnNumber int) string {
	return executionID.String() + "-turn-" + strconv.Itoa(turnNumber)
}

func NewExecutionTurn(executionID ExecutionID, turnNumber int, kind TurnKind, input string) ExecutionTurn {
	return ExecutionTurn{
		ID: NewExecutionTurnID(), ExecutionID: executionID, TurnNumber: turnNumber, Kind: kind,
		RequestID: TurnRequestID(executionID, turnNumber), Input: input, StartedAt: time.Now().UTC(),
	}
}

func (t ExecutionTurn) Completed(output string) ExecutionTurn {
	now := time.Now().UTC()
	t.Output = &output
	t.CompletedAt = &now
	return t
}

func (t ExecutionTurn) Failed(errMsg string) ExecutionTurn {
	now := time.Now().UTC()
	t.Error = &errMsg
	t.CompletedAt = &now
	return t
}
