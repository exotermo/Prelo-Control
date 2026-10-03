package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

type fakeSuspensionRepo struct {
	mu          sync.Mutex
	suspensions map[string]domain.ExecutionSuspension
}

func newFakeSuspensionRepo() *fakeSuspensionRepo {
	return &fakeSuspensionRepo{suspensions: map[string]domain.ExecutionSuspension{}}
}

func (f *fakeSuspensionRepo) Insert(_ context.Context, s domain.ExecutionSuspension) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suspensions[s.ID.String()] = s
	return nil
}

func (f *fakeSuspensionRepo) FindActiveByResumeKey(_ context.Context, reason domain.SuspensionReason, resumeKey string) (domain.ExecutionSuspension, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.suspensions {
		if s.Reason == reason && s.ResumeKey == resumeKey && s.ResolvedAt == nil {
			return s, true, nil
		}
	}
	return domain.ExecutionSuspension{}, false, nil
}

func (f *fakeSuspensionRepo) FindActiveByExecutionID(_ context.Context, executionID domain.ExecutionID) (domain.ExecutionSuspension, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.suspensions {
		if s.ExecutionID == executionID && s.ResolvedAt == nil {
			return s, true, nil
		}
	}
	return domain.ExecutionSuspension{}, false, nil
}

func (f *fakeSuspensionRepo) Resolve(_ context.Context, id domain.ExecutionSuspensionID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.suspensions[id.String()]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	s.ResolvedAt = &now
	f.suspensions[id.String()] = s
	return nil
}

func newTestPipelineHandler() (*PipelineHandler, *fakeTaskRepo, *fakeExecutionRepo, *fakeSuspensionRepo) {
	tasks := newFakeTaskRepo()
	executions := newFakeExecutionRepo()
	suspensions := newFakeSuspensionRepo()
	return NewPipelineHandler(tasks, executions, suspensions), tasks, executions, suspensions
}

func doPipelineGet(t *testing.T, h *PipelineHandler) []pipelineNodeResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var nodes []pipelineNodeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &nodes); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return nodes
}

func TestPipelineHandler_RunningTaskWithNoSuspension(t *testing.T) {
	h, tasks, executions, _ := newTestPipelineHandler()
	task := domain.Task{ID: domain.NewTaskID(), Status: domain.TaskRunning, AgentID: domain.AgentID{Value: "general"}}
	_ = tasks.Insert(context.Background(), task)
	_ = executions.Insert(context.Background(), domain.Execution{ID: domain.NewExecutionID(), TaskID: task.ID, Status: domain.ExecutionRunning})

	nodes := doPipelineGet(t, h)
	if len(nodes) != 1 || nodes[0].PipelineStatus != "RUNNING" {
		t.Fatalf("expected one RUNNING node, got %+v", nodes)
	}
}

func TestPipelineHandler_RunningTaskAwaitingApproval(t *testing.T) {
	h, tasks, executions, suspensions := newTestPipelineHandler()
	task := domain.Task{ID: domain.NewTaskID(), Status: domain.TaskRunning, AgentID: domain.AgentID{Value: "general"}}
	_ = tasks.Insert(context.Background(), task)
	execution := domain.Execution{ID: domain.NewExecutionID(), TaskID: task.ID, Status: domain.ExecutionRunning}
	_ = executions.Insert(context.Background(), execution)
	_ = suspensions.Insert(context.Background(), domain.NewExecutionSuspension(execution.ID, domain.SuspensionApproval, "some-approval-id"))

	nodes := doPipelineGet(t, h)
	if len(nodes) != 1 || nodes[0].PipelineStatus != "AWAITING_APPROVAL" {
		t.Fatalf("expected one AWAITING_APPROVAL node, got %+v", nodes)
	}
}

func TestPipelineHandler_RunningTaskAwaitingSubtask(t *testing.T) {
	h, tasks, executions, suspensions := newTestPipelineHandler()
	task := domain.Task{ID: domain.NewTaskID(), Status: domain.TaskRunning, AgentID: domain.AgentID{Value: "general"}}
	_ = tasks.Insert(context.Background(), task)
	execution := domain.Execution{ID: domain.NewExecutionID(), TaskID: task.ID, Status: domain.ExecutionRunning}
	_ = executions.Insert(context.Background(), execution)
	_ = suspensions.Insert(context.Background(), domain.NewExecutionSuspension(execution.ID, domain.SuspensionSubtask, "some-child-task-id"))

	nodes := doPipelineGet(t, h)
	if len(nodes) != 1 || nodes[0].PipelineStatus != "AWAITING_SUBTASK" {
		t.Fatalf("expected one AWAITING_SUBTASK node, got %+v", nodes)
	}
}

func TestPipelineHandler_CompletedChildUnderActiveRootPassesThrough(t *testing.T) {
	h, tasks, executions, _ := newTestPipelineHandler()
	root := domain.Task{ID: domain.NewTaskID(), Status: domain.TaskRunning, AgentID: domain.AgentID{Value: "general"}}
	childID := domain.NewTaskID()
	child := domain.Task{ID: childID, ParentTaskID: &root.ID, Status: domain.TaskCompleted, AgentID: domain.AgentID{Value: "general"}}
	_ = tasks.Insert(context.Background(), root)
	_ = tasks.Insert(context.Background(), child)
	_ = executions.Insert(context.Background(), domain.Execution{ID: domain.NewExecutionID(), TaskID: root.ID, Status: domain.ExecutionRunning})

	nodes := doPipelineGet(t, h)
	if len(nodes) != 1 || len(nodes[0].Children) != 1 {
		t.Fatalf("expected one root with one child, got %+v", nodes)
	}
	if nodes[0].Children[0].PipelineStatus != "COMPLETED" {
		t.Fatalf("expected child status COMPLETED, got %s", nodes[0].Children[0].PipelineStatus)
	}
}
