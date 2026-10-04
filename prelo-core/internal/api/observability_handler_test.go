package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/exotermo/prelo-core/internal/domain"
)

type fakeTurnRepo struct {
	turns map[string]domain.ExecutionTurn
}

func newFakeTurnRepo() *fakeTurnRepo { return &fakeTurnRepo{turns: map[string]domain.ExecutionTurn{}} }

func (f *fakeTurnRepo) Insert(_ context.Context, t domain.ExecutionTurn) (domain.ExecutionTurn, error) {
	f.turns[t.ID.String()] = t
	return t, nil
}
func (f *fakeTurnRepo) Update(_ context.Context, t domain.ExecutionTurn) (domain.ExecutionTurn, error) {
	f.turns[t.ID.String()] = t
	return t, nil
}
func (f *fakeTurnRepo) ListByExecution(_ context.Context, executionID domain.ExecutionID) ([]domain.ExecutionTurn, error) {
	var result []domain.ExecutionTurn
	for _, t := range f.turns {
		if t.ExecutionID == executionID {
			result = append(result, t)
		}
	}
	return result, nil
}
func (f *fakeTurnRepo) FindByRequestID(_ context.Context, requestID string) (domain.ExecutionTurn, bool, error) {
	for _, t := range f.turns {
		if t.RequestID == requestID {
			return t, true, nil
		}
	}
	return domain.ExecutionTurn{}, false, nil
}

func TestObservabilityHandler_Turns_ReturnsLedgerInOrder(t *testing.T) {
	taskRepo := newFakeTaskRepo()
	execRepo := newFakeExecutionRepo()
	turnRepo := newFakeTurnRepo()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(context.Background(), task)
	execution := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(context.Background(), execution)

	turn0 := domain.NewExecutionTurn(execution.ID, 0, domain.TurnLLMCall, "prompt")
	_, _ = turnRepo.Insert(context.Background(), turn0.Completed("tool_use:current_time({})"))
	turn1 := domain.NewExecutionTurn(execution.ID, 1, domain.TurnToolCall, "current_time({})")
	_, _ = turnRepo.Insert(context.Background(), turn1.Completed("2026-01-01T00:00:00Z"))

	handler := NewObservabilityHandler(taskRepo, execRepo, turnRepo)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks/{taskId}/executions/{executionId}/turns", handler.Turns)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+task.ID.String()+"/executions/"+execution.ID.String()+"/turns", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []turnResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(resp))
	}
	if resp[0].TurnNumber != 0 || resp[1].TurnNumber != 1 {
		t.Fatalf("expected turns ordered by turn number, got %+v", resp)
	}
}

func TestObservabilityHandler_Turns_WrongTaskIsNotFound(t *testing.T) {
	taskRepo := newFakeTaskRepo()
	execRepo := newFakeExecutionRepo()
	turnRepo := newFakeTurnRepo()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(context.Background(), task)
	otherTask, _ := domain.NewTask("other", agentID)
	_ = taskRepo.Insert(context.Background(), otherTask)
	execution := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(context.Background(), execution)

	handler := NewObservabilityHandler(taskRepo, execRepo, turnRepo)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks/{taskId}/executions/{executionId}/turns", handler.Turns)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+otherTask.ID.String()+"/executions/"+execution.ID.String()+"/turns", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestObservabilityHandler_Tree_BuildsParentChildStructure(t *testing.T) {
	taskRepo := newFakeTaskRepo()
	execRepo := newFakeExecutionRepo()
	turnRepo := newFakeTurnRepo()

	agentID, _ := domain.NewAgentID("general")
	parent, _ := domain.NewTask("parent", agentID)
	_ = taskRepo.Insert(context.Background(), parent)
	child, _ := domain.NewSubtask("child", agentID, parent)
	_ = taskRepo.Insert(context.Background(), child)

	parentExec := domain.NewPendingExecution(parent.ID, agentID)
	_ = execRepo.Insert(context.Background(), parentExec)

	handler := NewObservabilityHandler(taskRepo, execRepo, turnRepo)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks/{taskId}/tree", handler.Tree)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+parent.ID.String()+"/tree", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp taskTreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.TaskID != parent.ID.String() {
		t.Fatalf("expected root to be the parent, got %+v", resp)
	}
	if resp.ExecutionStatus == nil || *resp.ExecutionStatus != string(domain.ExecutionPending) {
		t.Fatalf("expected the parent's execution status attached, got %+v", resp.ExecutionStatus)
	}
	if len(resp.Children) != 1 || resp.Children[0].TaskID != child.ID.String() {
		t.Fatalf("expected exactly 1 child matching the delegated task, got %+v", resp.Children)
	}
	if resp.Children[0].Depth != 1 {
		t.Fatalf("expected child depth 1, got %d", resp.Children[0].Depth)
	}
}
