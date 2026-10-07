package domain

import (
	"time"

	"github.com/google/uuid"
)

type ToolCallID struct{ Value uuid.UUID }

func NewToolCallID() ToolCallID { return ToolCallID{Value: uuid.New()} }

func (id ToolCallID) String() string { return id.Value.String() }

// PermissionDecision is PermissionPolicy's verdict for one call, recorded the moment it is
// rendered — before the tool ever runs. It is never revisited: an approval later resolves the
// call's Outcome, but Decision itself stays exactly what the policy said at the time.
type PermissionDecision string

const (
	DecisionAllow           PermissionDecision = "ALLOW"
	DecisionDeny            PermissionDecision = "DENY"
	DecisionRequireApproval PermissionDecision = "REQUIRE_APPROVAL"
)

// ToolCallOutcome is nil until the call is actually resolved — either immediately (ALLOW/DENY)
// or later through an ApprovalRequest (REQUIRE_APPROVAL). A row with a nil Outcome is not a
// gap in the audit trail; it is itself the record of "decided REQUIRE_APPROVAL, still pending".
type ToolCallOutcome string

const (
	OutcomeExecuted ToolCallOutcome = "EXECUTED"
	OutcomeFailed   ToolCallOutcome = "FAILED"
	OutcomeDenied   ToolCallOutcome = "DENIED"
	OutcomeExpired  ToolCallOutcome = "EXPIRED"
)

// ToolCall is the audit record for every attempted tool invocation, allowed or not (ADR-004,
// AGENTS.md: "toda chamada é auditada" / "ferramentas e ações de risco passam pelo futuro
// PermissionPolicy"). It always carries the agent and tool that were involved and the args
// that were requested, regardless of what happened next.
type ToolCall struct {
	ID          ToolCallID
	TaskID      TaskID
	ExecutionID ExecutionID
	AgentID     AgentID
	ToolName    string
	ArgsJSON    string
	RiskLevel   RiskLevel
	Decision    PermissionDecision
	// ToolPolicyVersion binds a pending approval to the project's policy generation.
	ToolPolicyVersion int64
	Outcome           *ToolCallOutcome
	Result            *string
	Error             *string
	CreatedAt         time.Time
	ResolvedAt        *time.Time
	Version           int64
}

func NewToolCall(taskID TaskID, executionID ExecutionID, agentID AgentID, toolName, argsJSON string, riskLevel RiskLevel, decision PermissionDecision) ToolCall {
	return ToolCall{
		ID: NewToolCallID(), TaskID: taskID, ExecutionID: executionID, AgentID: agentID,
		ToolName: toolName, ArgsJSON: argsJSON, RiskLevel: riskLevel, Decision: decision,
		CreatedAt: time.Now().UTC(),
	}
}

// Resolved closes out the call — called at most once per call, whether that happens inline
// (ALLOW/DENY) or later from DecideApprovalUseCase once a human approves, rejects, or the
// request expires.
func (c ToolCall) Resolved(outcome ToolCallOutcome, result *string, errMsg *string) ToolCall {
	now := time.Now().UTC()
	c.Outcome = &outcome
	c.Result = result
	c.Error = errMsg
	c.ResolvedAt = &now
	return c
}

func (c ToolCall) WithVersion(v int64) ToolCall { c.Version = v; return c }
