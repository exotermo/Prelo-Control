package application

import (
	"context"
	"errors"
	"testing"

	"github.com/exotermo/prelo-core/internal/domain"
)

type fixedTaskReader struct{ task domain.Task }

func (f fixedTaskReader) FindByID(_ context.Context, _ domain.TaskID) (domain.Task, error) {
	return f.task, nil
}

type mutableAvailability struct {
	allowed bool
	err     error
	version int64
}

func (m *mutableAvailability) Allowed(_ context.Context, _ domain.ProjectID, _ string) (bool, error) {
	return m.allowed, m.err
}

func (m *mutableAvailability) Get(_ context.Context, _ domain.ProjectID, _ string) (ToolSetting, error) {
	return ToolSetting{Enabled: m.allowed, Version: m.version}, m.err
}

func projectTask(t *testing.T, executionID domain.ExecutionID, executions *fakeExecutions) domain.Task {
	t.Helper()
	execution := executions.executions[executionID.String()]
	task, err := domain.NewTask("verificar política", execution.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	task.ID = execution.TaskID
	project := domain.NewProjectID()
	task.ProjectID = &project
	return task
}

func TestToolAvailability_DeniedProjectToolIsNotOfferedOrInvoked(t *testing.T) {
	uc, calls, approvals, executions, executionID, low, _ := setupInvokeTool(t, []string{"current_time"})
	task := projectTask(t, executionID, executions)
	availability := &mutableAvailability{allowed: false}
	uc.SetToolAvailability(fixedTaskReader{task}, availability)
	agent, _ := uc.agents.FindRequired(task.AgentID)
	specs, err := toolSpecsForProject(context.Background(), agent, task.ProjectID, uc.tools, availability, false)
	if err != nil || len(specs) != 0 {
		t.Fatalf("blocked tool offered to model: %v, %v", specs, err)
	}
	call, approvalID, err := uc.Invoke(context.Background(), executionID, "current_time", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if call.Decision != domain.DecisionDeny || low.calls != 0 || approvalID != nil || len(approvals.approvals) != 0 || len(calls.calls) != 1 {
		t.Fatalf("blocked tool must be audited and denied without execution: %#v", call)
	}
}

func TestToolAvailability_StorageFailureFailsClosed(t *testing.T) {
	uc, calls, _, executions, executionID, low, _ := setupInvokeTool(t, []string{"current_time"})
	task := projectTask(t, executionID, executions)
	availability := &mutableAvailability{allowed: true, err: errors.New("storage unavailable")}
	uc.SetToolAvailability(fixedTaskReader{task}, availability)
	agent, _ := uc.agents.FindRequired(task.AgentID)
	if specs, err := toolSpecsForProject(context.Background(), agent, task.ProjectID, uc.tools, availability, false); err == nil || len(specs) != 0 {
		t.Fatalf("uncertain tool must not be offered: %v, %v", specs, err)
	}
	if _, _, err := uc.Invoke(context.Background(), executionID, "current_time", "{}"); err == nil || low.calls != 0 || len(calls.calls) != 0 {
		t.Fatalf("storage outage must not execute: %v", err)
	}
}

func TestToolAvailability_RevokedPendingApprovalDoesNotExecute(t *testing.T) {
	uc, calls, approvals, executions, executionID, _, moderate := setupInvokeTool(t, []string{"echo"})
	task := projectTask(t, executionID, executions)
	availability := &mutableAvailability{allowed: true}
	uc.SetToolAvailability(fixedTaskReader{task}, availability)
	_, approvalID, err := uc.Invoke(context.Background(), executionID, "echo", "{}")
	if err != nil || approvalID == nil {
		t.Fatalf("approval not created: %v", err)
	}
	availability.allowed = false
	availability.version++      // disabling advances policy generation
	availability.allowed = true // enabling later never revives the older pending request
	availability.version++
	decide := NewDecideApprovalUseCase(approvals, calls, uc.tools, executions, newFakeTurns(), newFakeSuspensions(), &fakeJobs{})
	decide.SetToolAvailability(fixedTaskReader{task}, availability)
	if _, err := decide.Approve(context.Background(), *approvalID, "admin"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked approval should fail closed, got %v", err)
	}
	if moderate.calls != 0 || approvals.approvals[approvalID.String()].Status != domain.ApprovalPending {
		t.Fatal("revoked tool was executed or approval changed")
	}
}
