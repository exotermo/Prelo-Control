package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

type fakeExecutions struct {
	executions map[string]domain.Execution
}

func (f *fakeExecutions) Insert(context.Context, domain.Execution) error { return nil }
func (f *fakeExecutions) FindByID(_ context.Context, id domain.ExecutionID) (domain.Execution, error) {
	e, ok := f.executions[id.String()]
	if !ok {
		return domain.Execution{}, ErrExecutionNotFound
	}
	return e, nil
}
func (f *fakeExecutions) Update(_ context.Context, e domain.Execution) (domain.Execution, error) {
	f.executions[e.ID.String()] = e
	return e, nil
}
func (f *fakeExecutions) FindByTaskID(_ context.Context, taskID domain.TaskID) (domain.Execution, error) {
	for _, e := range f.executions {
		if e.TaskID == taskID {
			return e, nil
		}
	}
	return domain.Execution{}, ErrExecutionNotFound
}

type fakeAgents struct {
	agents map[string]domain.AgentDefinition
}

func (f *fakeAgents) Find(id domain.AgentID) (domain.AgentDefinition, bool) {
	a, ok := f.agents[id.String()]
	return a, ok
}
func (f *fakeAgents) FindRequired(id domain.AgentID) (domain.AgentDefinition, error) {
	a, ok := f.Find(id)
	if !ok {
		return domain.AgentDefinition{}, &domain.ErrUnknownAgent{AgentID: id.String()}
	}
	return a, nil
}

type fakeExecutor struct {
	def    domain.ToolDefinition
	result string
	err    error
	calls  int
	// block, when true, makes Execute wait for ctx to be done instead of returning immediately —
	// used to prove a hung tool is bounded by defaultToolExecutionTimeout rather than running forever.
	block bool
}

func (f *fakeExecutor) Definition() domain.ToolDefinition { return f.def }
func (f *fakeExecutor) Execute(ctx context.Context, _ domain.Execution, _ string) (string, error) {
	f.calls++
	if f.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.result, f.err
}

type fakeToolRegistry struct {
	tools map[string]ToolExecutor
}

func (f *fakeToolRegistry) Find(name string) (ToolExecutor, bool) {
	t, ok := f.tools[name]
	return t, ok
}
func (f *fakeToolRegistry) List() []domain.ToolDefinition {
	defs := make([]domain.ToolDefinition, 0, len(f.tools))
	for _, t := range f.tools {
		defs = append(defs, t.Definition())
	}
	return defs
}

type fakeToolCalls struct {
	calls map[string]domain.ToolCall
}

func (f *fakeToolCalls) Insert(_ context.Context, c domain.ToolCall) error {
	f.calls[c.ID.String()] = c
	return nil
}
func (f *fakeToolCalls) FindByID(_ context.Context, id domain.ToolCallID) (domain.ToolCall, error) {
	c, ok := f.calls[id.String()]
	if !ok {
		return domain.ToolCall{}, ErrToolCallNotFound
	}
	return c, nil
}
func (f *fakeToolCalls) Update(_ context.Context, c domain.ToolCall) (domain.ToolCall, error) {
	f.calls[c.ID.String()] = c
	return c, nil
}

type fakeApprovals struct {
	approvals map[string]domain.ApprovalRequest
}

func (f *fakeApprovals) Insert(_ context.Context, a domain.ApprovalRequest) error {
	f.approvals[a.ID.String()] = a
	return nil
}
func (f *fakeApprovals) FindByID(_ context.Context, id domain.ApprovalRequestID) (domain.ApprovalRequest, error) {
	a, ok := f.approvals[id.String()]
	if !ok {
		return domain.ApprovalRequest{}, ErrApprovalNotFound
	}
	return a, nil
}
func (f *fakeApprovals) Update(_ context.Context, a domain.ApprovalRequest) (domain.ApprovalRequest, error) {
	f.approvals[a.ID.String()] = a
	return a, nil
}
func (f *fakeApprovals) ListPending(context.Context) ([]domain.ApprovalRequest, error) {
	var result []domain.ApprovalRequest
	for _, a := range f.approvals {
		if a.Status == domain.ApprovalPending {
			result = append(result, a)
		}
	}
	return result, nil
}
func (f *fakeApprovals) ListPendingByProject(context.Context, *uuid.UUID) ([]domain.ApprovalRequest, error) {
	return f.ListPending(context.Background())
}

type fakeTurns struct {
	turns map[string]domain.ExecutionTurn
}

func newFakeTurns() *fakeTurns { return &fakeTurns{turns: map[string]domain.ExecutionTurn{}} }

func (f *fakeTurns) Insert(_ context.Context, t domain.ExecutionTurn) (domain.ExecutionTurn, error) {
	f.turns[t.ID.String()] = t
	return t, nil
}
func (f *fakeTurns) Update(_ context.Context, t domain.ExecutionTurn) (domain.ExecutionTurn, error) {
	f.turns[t.ID.String()] = t
	return t, nil
}
func (f *fakeTurns) ListByExecution(_ context.Context, executionID domain.ExecutionID) ([]domain.ExecutionTurn, error) {
	var result []domain.ExecutionTurn
	for _, t := range f.turns {
		if t.ExecutionID == executionID {
			result = append(result, t)
		}
	}
	return result, nil
}
func (f *fakeTurns) FindByRequestID(_ context.Context, requestID string) (domain.ExecutionTurn, bool, error) {
	for _, t := range f.turns {
		if t.RequestID == requestID {
			return t, true, nil
		}
	}
	return domain.ExecutionTurn{}, false, nil
}

type fakeSuspensions struct {
	suspensions map[string]domain.ExecutionSuspension
}

func newFakeSuspensions() *fakeSuspensions {
	return &fakeSuspensions{suspensions: map[string]domain.ExecutionSuspension{}}
}
func (f *fakeSuspensions) Insert(_ context.Context, s domain.ExecutionSuspension) error {
	f.suspensions[s.ID.String()] = s
	return nil
}
func (f *fakeSuspensions) FindActiveByResumeKey(_ context.Context, reason domain.SuspensionReason, resumeKey string) (domain.ExecutionSuspension, bool, error) {
	for _, s := range f.suspensions {
		if s.Reason == reason && s.ResumeKey == resumeKey && s.ResolvedAt == nil {
			return s, true, nil
		}
	}
	return domain.ExecutionSuspension{}, false, nil
}
func (f *fakeSuspensions) FindActiveByExecutionID(_ context.Context, executionID domain.ExecutionID) (domain.ExecutionSuspension, bool, error) {
	for _, s := range f.suspensions {
		if s.ExecutionID == executionID && s.ResolvedAt == nil {
			return s, true, nil
		}
	}
	return domain.ExecutionSuspension{}, false, nil
}
func (f *fakeSuspensions) Resolve(_ context.Context, id domain.ExecutionSuspensionID) error {
	s := f.suspensions[id.String()]
	if s.ResolvedAt != nil {
		return ErrExecutionSuspensionAlreadyResolved
	}
	s = s.Resolved()
	f.suspensions[id.String()] = s
	return nil
}

// fakeJobs is a minimal ExecutionJobRepository — DecideApprovalUseCase only ever calls Resume,
// but the interface needs a full implementation.
type fakeJobs struct{ resumed []string }

func (f *fakeJobs) Insert(context.Context, domain.ExecutionJob) error { return nil }
func (f *fakeJobs) FindByID(context.Context, domain.ExecutionID) (domain.ExecutionJob, error) {
	return domain.ExecutionJob{}, nil
}
func (f *fakeJobs) Claim(context.Context, domain.ExecutionID, string, time.Duration) (domain.ExecutionJob, error) {
	return domain.ExecutionJob{}, nil
}
func (f *fakeJobs) MarkRunning(context.Context, domain.ExecutionID, time.Duration) error { return nil }
func (f *fakeJobs) MarkDone(context.Context, domain.ExecutionID) error                   { return nil }
func (f *fakeJobs) MarkFailed(context.Context, domain.ExecutionID, string) error         { return nil }
func (f *fakeJobs) Suspend(context.Context, domain.ExecutionID) error                    { return nil }
func (f *fakeJobs) Resume(_ context.Context, id domain.ExecutionID) error {
	f.resumed = append(f.resumed, id.String())
	return nil
}
func (f *fakeJobs) ListClaimable(context.Context, int) ([]domain.ExecutionID, error) { return nil, nil }
func (f *fakeJobs) RequeueOrphaned(context.Context) ([]domain.ExecutionID, error)    { return nil, nil }

func setupInvokeTool(t *testing.T, capabilities []string) (*InvokeToolUseCase, *fakeToolCalls, *fakeApprovals, *fakeExecutions, domain.ExecutionID, *fakeExecutor, *fakeExecutor) {
	t.Helper()
	agentID, _ := domain.NewAgentID("general")
	agent, err := domain.NewAgentDefinition(agentID, domain.AgentTypeGeneral, "1", "directive", "profile", capabilities, "General", "desc")
	if err != nil {
		t.Fatalf("failed to build agent: %v", err)
	}

	execution := domain.NewPendingExecution(domain.NewTaskID(), agentID)
	executions := &fakeExecutions{executions: map[string]domain.Execution{execution.ID.String(): execution}}
	agents := &fakeAgents{agents: map[string]domain.AgentDefinition{"general": agent}}

	lowDef, _ := domain.NewToolDefinition("current_time", "desc", domain.RiskLow)
	lowTool := &fakeExecutor{def: lowDef, result: "2026-01-01T00:00:00Z"}
	highDef, _ := domain.NewToolDefinition("echo", "desc", domain.RiskModerate)
	highTool := &fakeExecutor{def: highDef, result: "echoed"}
	registry := &fakeToolRegistry{tools: map[string]ToolExecutor{"current_time": lowTool, "echo": highTool}}

	calls := &fakeToolCalls{calls: map[string]domain.ToolCall{}}
	approvals := &fakeApprovals{approvals: map[string]domain.ApprovalRequest{}}
	uc := NewInvokeToolUseCase(executions, agents, registry, NewDefaultPermissionPolicy(), calls, approvals)
	return uc, calls, approvals, executions, execution.ID, lowTool, highTool
}

func TestInvokeTool_LowRiskWithCapability_ExecutesImmediately(t *testing.T) {
	uc, _, _, _, executionID, lowTool, _ := setupInvokeTool(t, []string{"current_time"})

	call, _, err := uc.Invoke(context.Background(), executionID, "current_time", "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if call.Decision != domain.DecisionAllow {
		t.Fatalf("expected ALLOW, got %s", call.Decision)
	}
	if call.Outcome == nil || *call.Outcome != domain.OutcomeExecuted {
		t.Fatalf("expected EXECUTED outcome, got %v", call.Outcome)
	}
	if lowTool.calls != 1 {
		t.Fatalf("expected the tool to run exactly once, ran %d times", lowTool.calls)
	}
}

func TestInvokeTool_WithoutCapability_IsDeniedAndNeverRuns(t *testing.T) {
	uc, _, _, _, executionID, lowTool, _ := setupInvokeTool(t, []string{}) // no capabilities granted

	call, _, err := uc.Invoke(context.Background(), executionID, "current_time", "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if call.Decision != domain.DecisionDeny {
		t.Fatalf("expected DENY, got %s", call.Decision)
	}
	if call.Outcome == nil || *call.Outcome != domain.OutcomeDenied {
		t.Fatalf("expected DENIED outcome, got %v", call.Outcome)
	}
	if lowTool.calls != 0 {
		t.Fatalf("expected the tool to never run, ran %d times", lowTool.calls)
	}
}

func TestInvokeTool_ModerateRisk_RequiresApprovalAndDoesNotRunYet(t *testing.T) {
	uc, calls, approvals, _, executionID, _, highTool := setupInvokeTool(t, []string{"echo"})

	call, _, err := uc.Invoke(context.Background(), executionID, "echo", `{"msg":"hi"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if call.Decision != domain.DecisionRequireApproval {
		t.Fatalf("expected REQUIRE_APPROVAL, got %s", call.Decision)
	}
	if call.Outcome != nil {
		t.Fatalf("expected no outcome yet, got %v", call.Outcome)
	}
	if highTool.calls != 0 {
		t.Fatalf("expected the tool to not run before approval, ran %d times", highTool.calls)
	}
	if len(calls.calls) != 1 {
		t.Fatalf("expected exactly one audited ToolCall row, got %d", len(calls.calls))
	}
	if len(approvals.approvals) != 1 {
		t.Fatalf("expected exactly one ApprovalRequest row, got %d", len(approvals.approvals))
	}
}

func TestDecideApproval_Approve_RunsTheToolExactlyOnce(t *testing.T) {
	uc, calls, approvals, executions, executionID, _, highTool := setupInvokeTool(t, []string{"echo"})
	call, _, _ := uc.Invoke(context.Background(), executionID, "echo", `{"msg":"hi"}`)

	var approvalID domain.ApprovalRequestID
	for _, a := range approvals.approvals {
		if a.ToolCallID == call.ID {
			approvalID = a.ID
		}
	}

	decide := NewDecideApprovalUseCase(approvals, calls, &fakeToolRegistry{tools: map[string]ToolExecutor{"echo": highTool}}, executions, newFakeTurns(), newFakeSuspensions(), &fakeJobs{})
	approved, err := decide.Approve(context.Background(), approvalID, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved.Status != domain.ApprovalApproved {
		t.Fatalf("expected APPROVED, got %s", approved.Status)
	}
	if highTool.calls != 1 {
		t.Fatalf("expected the tool to run exactly once after approval, ran %d times", highTool.calls)
	}
	resolved := calls.calls[call.ID.String()]
	if resolved.Outcome == nil || *resolved.Outcome != domain.OutcomeExecuted {
		t.Fatalf("expected the ToolCall to resolve to EXECUTED, got %v", resolved.Outcome)
	}

	// Approving twice must fail — the tool must not run a second time for the same approval.
	if _, err := decide.Approve(context.Background(), approvalID, "bob"); err == nil {
		t.Fatal("expected an error approving an already-decided approval")
	}
	if highTool.calls != 1 {
		t.Fatalf("expected the tool to still have run only once, ran %d times", highTool.calls)
	}
}

func TestInvokeTool_HungTool_IsBoundedByTimeout(t *testing.T) {
	original := defaultToolExecutionTimeout
	defaultToolExecutionTimeout = 20 * time.Millisecond
	defer func() { defaultToolExecutionTimeout = original }()

	uc, _, _, _, executionID, lowTool, _ := setupInvokeTool(t, []string{"current_time"})
	lowTool.block = true

	call, _, err := uc.Invoke(context.Background(), executionID, "current_time", "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if call.Decision != domain.DecisionAllow {
		t.Fatalf("expected ALLOW, got %s", call.Decision)
	}
	if call.Outcome == nil || *call.Outcome != domain.OutcomeFailed {
		t.Fatalf("expected FAILED outcome after timeout, got %v", call.Outcome)
	}
	if call.Error == nil || *call.Error != context.DeadlineExceeded.Error() {
		t.Fatalf("expected deadline exceeded error, got %v", call.Error)
	}
}

func TestInvokeTool_RejectsArgumentsOverLimitBeforeExecution(t *testing.T) {
	uc, _, _, executions, executionID, lowTool, _ := setupInvokeTool(t, []string{"current_time"})
	uc.limits.MaxArgsBytes = 3
	call, _, err := uc.Invoke(context.Background(), executionID, "current_time", "{}xx")
	if err == nil {
		t.Fatal("expected argument limit error")
	}
	if _, ok := err.(*ToolLimitError); !ok {
		t.Fatalf("expected ToolLimitError, got %T", err)
	}
	if call.ID != (domain.ToolCallID{}) || lowTool.calls != 0 || len(executions.executions) != 1 {
		t.Fatalf("tool executed or call persisted unexpectedly")
	}
}

func TestInvokeTool_MarksOversizedResultFailed(t *testing.T) {
	uc, calls, _, _, executionID, lowTool, _ := setupInvokeTool(t, []string{"current_time"})
	uc.limits.MaxResultBytes = 2
	lowTool.result = "too large"
	call, _, err := uc.Invoke(context.Background(), executionID, "current_time", "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if call.Outcome == nil || *call.Outcome != domain.OutcomeFailed {
		t.Fatalf("expected failed outcome, got %+v", call)
	}
	if call.Result != nil || call.Error == nil {
		t.Fatalf("oversized result must not be persisted: %+v", call)
	}
	if _, ok := calls.calls[call.ID.String()]; !ok {
		t.Fatal("tool call audit was not persisted")
	}
}

func TestDecideApproval_Deny_NeverRunsTheTool(t *testing.T) {
	uc, calls, approvals, executions, executionID, _, highTool := setupInvokeTool(t, []string{"echo"})
	call, _, _ := uc.Invoke(context.Background(), executionID, "echo", `{"msg":"hi"}`)

	var approvalID domain.ApprovalRequestID
	for _, a := range approvals.approvals {
		if a.ToolCallID == call.ID {
			approvalID = a.ID
		}
	}

	decide := NewDecideApprovalUseCase(approvals, calls, &fakeToolRegistry{tools: map[string]ToolExecutor{"echo": highTool}}, executions, newFakeTurns(), newFakeSuspensions(), &fakeJobs{})
	denied, err := decide.Deny(context.Background(), approvalID, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if denied.Status != domain.ApprovalDenied {
		t.Fatalf("expected DENIED, got %s", denied.Status)
	}
	if highTool.calls != 0 {
		t.Fatalf("expected the tool to never run, ran %d times", highTool.calls)
	}
	resolved := calls.calls[call.ID.String()]
	if resolved.Outcome == nil || *resolved.Outcome != domain.OutcomeDenied {
		t.Fatalf("expected the ToolCall to resolve to DENIED, got %v", resolved.Outcome)
	}
}
