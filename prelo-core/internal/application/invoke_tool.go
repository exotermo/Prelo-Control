package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
)

// defaultApprovalTTL is how long a REQUIRE_APPROVAL request stays decidable before it reads as
// EXPIRED (etapa 8's "expira corretamente"). Not yet configurable per tool/tenant — every
// moderate-or-higher call gets the same window until a real need to vary it shows up.
const defaultApprovalTTL = 15 * time.Minute

// ToolExecutionLimits is a defense-in-depth quota for in-process tools. It is not a substitute
// for an OS sandbox: tools that execute processes or access a filesystem must remain denied until
// they run in a dedicated sandbox/worker.
type ToolExecutionLimits struct {
	Timeout        time.Duration
	MaxArgsBytes   int
	MaxResultBytes int
	MaxConcurrent  int
}

// Kept as a package variable for backwards-compatible tests and for the existing approval
// path; production code should prefer NewInvokeToolUseCaseWithLimits.
var defaultToolExecutionTimeout = 30 * time.Second
var defaultToolSemaphore = make(chan struct{}, 16)

func DefaultToolExecutionLimits() ToolExecutionLimits {
	return ToolExecutionLimits{Timeout: defaultToolExecutionTimeout, MaxArgsBytes: 64 * 1024, MaxResultBytes: 64 * 1024, MaxConcurrent: 16}
}

// InvokeToolUseCase is the only path that runs a tool. Every call — allowed, denied, or
// pending approval — is written to ToolCallRepository before anything else happens, so the
// audit trail never has a gap between "decided" and "recorded" (ADR-004, AGENTS.md).
type InvokeToolUseCase struct {
	executions ExecutionRepository
	agents     AgentRegistry
	tools      ToolRegistry
	policy     PermissionPolicy
	calls      ToolCallRepository
	approvals  ApprovalRepository
	limits     ToolExecutionLimits
	semaphore  chan struct{}
}

func NewInvokeToolUseCase(executions ExecutionRepository, agents AgentRegistry, tools ToolRegistry, policy PermissionPolicy, calls ToolCallRepository, approvals ApprovalRepository) *InvokeToolUseCase {
	return NewInvokeToolUseCaseWithLimits(executions, agents, tools, policy, calls, approvals, DefaultToolExecutionLimits())
}

func NewInvokeToolUseCaseWithLimits(executions ExecutionRepository, agents AgentRegistry, tools ToolRegistry, policy PermissionPolicy, calls ToolCallRepository, approvals ApprovalRepository, limits ToolExecutionLimits) *InvokeToolUseCase {
	if limits.Timeout <= 0 || limits.Timeout > 5*time.Minute {
		limits.Timeout = 30 * time.Second
	}
	if limits.MaxArgsBytes <= 0 {
		limits.MaxArgsBytes = 64 * 1024
	}
	if limits.MaxResultBytes <= 0 {
		limits.MaxResultBytes = 64 * 1024
	}
	if limits.MaxConcurrent <= 0 {
		limits.MaxConcurrent = 1
	}
	return &InvokeToolUseCase{executions: executions, agents: agents, tools: tools, policy: policy, calls: calls, approvals: approvals, limits: limits, semaphore: make(chan struct{}, limits.MaxConcurrent)}
}

// Invoke evaluates permission for exactly one call against the Execution's agent and, only
// when the decision is ALLOW, executes the tool inline. A REQUIRE_APPROVAL decision persists
// the ToolCall and its ApprovalRequest and returns without running anything at all — the
// returned *domain.ApprovalRequestID is non-nil only in that case, and
// DecideApprovalUseCase is the only thing that can move it forward from there.
func (uc *InvokeToolUseCase) Invoke(ctx context.Context, executionID domain.ExecutionID, toolName, argsJSON string) (domain.ToolCall, *domain.ApprovalRequestID, error) {
	execution, err := uc.executions.FindByID(ctx, executionID)
	if err != nil {
		return domain.ToolCall{}, nil, err
	}

	agent, err := uc.agents.FindRequired(execution.AgentID)
	if err != nil {
		return domain.ToolCall{}, nil, err
	}

	executor, ok := uc.tools.Find(toolName)
	if !ok {
		return domain.ToolCall{}, nil, ErrToolNotFound
	}
	toolDef := executor.Definition()
	if len(argsJSON) > uc.limits.MaxArgsBytes {
		return domain.ToolCall{}, nil, &ToolLimitError{Kind: "arguments", Limit: uc.limits.MaxArgsBytes}
	}

	decision := domain.DecisionDeny
	if hasCapability(agent, toolDef.Name) {
		decision = uc.policy.Evaluate(agent, toolDef)
	}

	call := domain.NewToolCall(execution.TaskID, execution.ID, agent.AgentID, toolDef.Name, argsJSON, toolDef.RiskLevel, decision)
	if err := uc.calls.Insert(ctx, call); err != nil {
		return domain.ToolCall{}, nil, err
	}

	switch decision {
	case domain.DecisionDeny:
		outcome := domain.OutcomeDenied
		resolved, err := uc.calls.Update(ctx, call.Resolved(outcome, nil, nil))
		return resolved, nil, err
	case domain.DecisionRequireApproval:
		scope := fmt.Sprintf("agent %s requests tool %q (risk %s) with args %s", agent.AgentID.String(), toolDef.Name, toolDef.RiskLevel, argsJSON)
		approval := domain.NewApprovalRequest(call.ID, scope, defaultApprovalTTL)
		if err := uc.approvals.Insert(ctx, approval); err != nil {
			return domain.ToolCall{}, nil, err
		}
		approvalID := approval.ID
		return call, &approvalID, nil
	case domain.DecisionAllow:
		resolved, err := uc.executeAndResolve(ctx, executor, execution, call)
		return resolved, nil, err
	default:
		return domain.ToolCall{}, nil, &domain.ValidationError{Message: "unknown permission decision"}
	}
}

func hasCapability(agent domain.AgentDefinition, toolName string) bool {
	for _, capability := range agent.Capabilities {
		if capability == toolName {
			return true
		}
	}
	return false
}

func (uc *InvokeToolUseCase) executeAndResolve(ctx context.Context, executor ToolExecutor, execution domain.Execution, call domain.ToolCall) (domain.ToolCall, error) {
	select {
	case uc.semaphore <- struct{}{}:
		defer func() { <-uc.semaphore }()
	case <-ctx.Done():
		message := ctx.Err().Error()
		return uc.calls.Update(ctx, call.Resolved(domain.OutcomeFailed, nil, &message))
	}
	execCtx, cancel := context.WithTimeout(ctx, uc.limits.Timeout)
	result, err := executor.Execute(execCtx, execution, call.ArgsJSON)
	cancel()
	// Persist the resolution against the caller's own context, not execCtx — execCtx may already
	// be past its deadline on the timeout path, and that must not block recording the outcome.
	if err != nil {
		message := err.Error()
		outcome := domain.OutcomeFailed
		return uc.calls.Update(ctx, call.Resolved(outcome, nil, &message))
	}
	if len(result) > uc.limits.MaxResultBytes {
		message := (&ToolLimitError{Kind: "result", Limit: uc.limits.MaxResultBytes}).Error()
		return uc.calls.Update(ctx, call.Resolved(domain.OutcomeFailed, nil, &message))
	}
	outcome := domain.OutcomeExecuted
	return uc.calls.Update(ctx, call.Resolved(outcome, &result, nil))
}

// executeAndResolve is also used by the approval use case, which predates the configurable
// limits constructor. Keep it as a safe default wrapper so approved calls receive the same
// timeout/output protections as direct calls.
func executeAndResolve(ctx context.Context, calls ToolCallRepository, executor ToolExecutor, execution domain.Execution, call domain.ToolCall) (domain.ToolCall, error) {
	limits := DefaultToolExecutionLimits()
	uc := &InvokeToolUseCase{calls: calls, limits: limits, semaphore: defaultToolSemaphore}
	return uc.executeAndResolve(ctx, executor, execution, call)
}

type ToolLimitError struct {
	Kind  string
	Limit int
}

func (e *ToolLimitError) Error() string {
	return fmt.Sprintf("tool %s exceeded the %d-byte limit", e.Kind, e.Limit)
}

func isToolTimeout(err error) bool { return errors.Is(err, context.DeadlineExceeded) }
