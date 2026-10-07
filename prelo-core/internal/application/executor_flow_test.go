package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/google/uuid"
)

type executorRequestsFake struct {
	inserted        []ExecutorRequest
	claimedCapacity ExecutorCapacity
}

func (f *executorRequestsFake) Insert(_ context.Context, q ExecutorRequest) error {
	f.inserted = append(f.inserted, q)
	return nil
}
func (f *executorRequestsFake) Find(_ context.Context, id uuid.UUID) (ExecutorRequest, error) {
	for _, q := range f.inserted {
		if q.ID == id {
			return q, nil
		}
	}
	return ExecutorRequest{}, ErrExecutorRequestNotFound
}
func (f *executorRequestsFake) ListByProject(context.Context, domain.ProjectID) ([]ExecutorRequest, error) {
	return f.inserted, nil
}
func (f *executorRequestsFake) Decide(context.Context, uuid.UUID, uuid.UUID, bool) (ExecutorRequest, error) {
	return ExecutorRequest{}, nil
}
func (f *executorRequestsFake) Claim(_ context.Context, _ uuid.UUID, _ uuid.UUID, capacity ExecutorCapacity) (*ExecutorJob, error) {
	f.claimedCapacity = capacity
	return nil, nil
}
func (f *executorRequestsFake) Current(context.Context, uuid.UUID, uuid.UUID) (ExecutorJob, error) {
	return ExecutorJob{}, nil
}
func (f *executorRequestsFake) Report(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int, string, json.RawMessage) error {
	return nil
}
func (f *executorRequestsFake) LinkToolCall(_ context.Context, requestID, toolCallID uuid.UUID) error {
	for i := range f.inserted {
		if f.inserted[i].ID == requestID {
			f.inserted[i].ToolCallID = &toolCallID
			return nil
		}
	}
	return ErrExecutorRequestNotFound
}
func (f *executorRequestsFake) LinkApproval(_ context.Context, requestID, approvalID uuid.UUID) error {
	for i := range f.inserted {
		if f.inserted[i].ID == requestID {
			f.inserted[i].ApprovalID = &approvalID
			return nil
		}
	}
	return ErrExecutorRequestNotFound
}
func (f *executorRequestsFake) FindByToolCall(_ context.Context, toolCallID uuid.UUID) (ExecutorRequest, error) {
	for _, q := range f.inserted {
		if q.ToolCallID != nil && *q.ToolCallID == toolCallID {
			return q, nil
		}
	}
	return ExecutorRequest{}, ErrExecutorRequestNotFound
}
func (f *executorRequestsFake) FindJob(context.Context, uuid.UUID) (ExecutorJob, error) {
	return ExecutorJob{}, ErrExecutorRequestNotFound
}
func (f *executorRequestsFake) MarkTaskNotified(context.Context, uuid.UUID) error { return nil }

type oneTask struct{ task domain.Task }

func (f oneTask) FindByID(context.Context, domain.TaskID) (domain.Task, error) { return f.task, nil }

type oneExecution struct{ execution domain.Execution }

func (f oneExecution) FindByID(context.Context, domain.ExecutionID) (domain.Execution, error) {
	return f.execution, nil
}

type oneWorker struct{ worker ExecutorWorker }

func (f oneWorker) FindByID(context.Context, uuid.UUID) (ExecutorWorker, error) { return f.worker, nil }

type executorAvailabilityFake struct{}

func (executorAvailabilityFake) Get(context.Context, domain.ProjectID, string) (ToolSetting, error) {
	return ToolSetting{Enabled: true, Version: 1}, nil
}
func (executorAvailabilityFake) Allowed(context.Context, domain.ProjectID, string) (bool, error) {
	return true, nil
}

func TestExecutorFlowCreatesOnlyProjectBoundExactPendingRequest(t *testing.T) {
	project := domain.ProjectID{Value: uuid.New()}
	taskID := domain.NewTaskID()
	executionID := domain.NewExecutionID()
	task := domain.Task{ID: taskID, ProjectID: &project, Status: domain.TaskRunning}
	execution := domain.Execution{ID: executionID, TaskID: taskID, Status: domain.ExecutionRunning}
	worker := ExecutorWorker{ID: uuid.New(), ProjectID: project, ImageDigest: "sha256:" + strings.Repeat("a", 64), Enabled: true}
	fake := &executorRequestsFake{}
	flow := NewExecutorFlow(fake, oneTask{task}, oneExecution{execution}, oneWorker{worker})
	flow.SetToolAvailability(executorAvailabilityFake{})
	args := ExecutorArgs{Path: "notes.txt", ContentBase64: "bm90YQ=="}
	q, err := flow.Create(context.Background(), project, taskID, executionID, worker.ID, "CREATE", args, "user")
	if err != nil {
		t.Fatal(err)
	}
	if q.Status != "PENDING" || q.Payload.Args != args || q.PayloadHash != ExecutorPayloadHash(q.Payload) || len(fake.inserted) != 1 {
		t.Fatal("request was not immutable and pending")
	}
	if _, err := flow.Create(context.Background(), project, taskID, executionID, worker.ID, "CREATE", ExecutorArgs{Path: "../secret", ContentBase64: "bm90YQ=="}, "user"); err == nil {
		t.Fatal("accepted path traversal")
	}
	if _, err := flow.Create(context.Background(), domain.ProjectID{Value: uuid.New()}, taskID, executionID, worker.ID, "CREATE", args, "user"); !errors.Is(err, ErrExecutorUnauthorized) {
		t.Fatal("accepted cross-project request")
	}
	if len(fake.inserted) != 1 {
		t.Fatal("unsafe request was persisted")
	}
}

func TestExecutorFlowRejectsTerminalTaskAndInvalidOperation(t *testing.T) {
	project := domain.ProjectID{Value: uuid.New()}
	taskID := domain.NewTaskID()
	executionID := domain.NewExecutionID()
	task := domain.Task{ID: taskID, ProjectID: &project, Status: domain.TaskCompleted}
	execution := domain.Execution{ID: executionID, TaskID: taskID, Status: domain.ExecutionCompleted}
	worker := ExecutorWorker{ID: uuid.New(), ProjectID: project, ImageDigest: "sha256:" + strings.Repeat("a", 64), Enabled: true}
	flow := NewExecutorFlow(&executorRequestsFake{}, oneTask{task}, oneExecution{execution}, oneWorker{worker})
	flow.SetToolAvailability(executorAvailabilityFake{})
	if _, err := flow.Create(context.Background(), project, taskID, executionID, worker.ID, "START_WORKSPACE", ExecutorArgs{}, "user"); !errors.Is(err, ErrExecutorConflict) {
		t.Fatal("terminal task accepted")
	}
	if _, err := NormalizeExecutorArgs("SHELL", ExecutorArgs{}); err == nil {
		t.Fatal("arbitrary operation accepted")
	}
}
