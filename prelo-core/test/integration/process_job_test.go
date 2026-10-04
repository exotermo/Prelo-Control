package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/agentregistry"
	"github.com/exotermo/prelo-core/internal/infrastructure/gateway"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
	"github.com/exotermo/prelo-core/internal/infrastructure/toolregistry"
)

type fakeLlmGateway struct {
	resp gateway.ChatResponse
	err  error
}

func (f fakeLlmGateway) Chat(context.Context, gateway.ChatRequest, string) (gateway.ChatResponse, error) {
	return f.resp, f.err
}

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, string) {}

type processJobFixture struct {
	taskRepo *postgres.TaskRepository
	enqueue  *application.EnqueueExecutionUseCase
	process  *application.ProcessJobUseCase
}

func setupProcessJobFixture(t *testing.T, llm application.LanguageModelGateway) processJobFixture {
	t.Helper()
	pool := newTestPool(t)

	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	jobRepo := postgres.NewExecutionJobRepository(pool)
	manualRepo := postgres.NewManualContextRepository(pool)
	snapshotRepo := postgres.NewContextSnapshotRepository(pool)
	resolver := application.NewContextResolver(manualRepo, snapshotRepo)
	agents, err := agentregistry.LoadDefault()
	if err != nil {
		t.Fatalf("failed to load agent catalog: %v", err)
	}

	enqueue := application.NewEnqueueExecutionUseCase(taskRepo, execRepo, jobRepo, noopPublisher{})

	toolCallRepo := postgres.NewToolCallRepository(pool)
	approvalRepo := postgres.NewApprovalRepository(pool)
	turnRepo := postgres.NewExecutionTurnRepository(pool)
	suspensionRepo := postgres.NewExecutionSuspensionRepository(pool)
	emptyTools := toolregistry.NewStatic() // no tools needed — these fixtures never trigger TOOL_USE
	invokeTool := application.NewInvokeToolUseCase(execRepo, agents, emptyTools, application.NewDefaultPermissionPolicy(), toolCallRepo, approvalRepo)
	agentLoop := application.NewRunAgentLoopUseCase(llm, emptyTools, invokeTool, turnRepo, suspensionRepo, jobRepo)
	process := application.NewProcessJobUseCase(taskRepo, execRepo, jobRepo, agents, resolver, snapshotRepo, agentLoop, turnRepo, suspensionRepo, "test-worker")

	return processJobFixture{taskRepo: taskRepo, enqueue: enqueue, process: process}
}

func TestProcessJobUseCase_HappyPath(t *testing.T) {
	llm := fakeLlmGateway{resp: gateway.ChatResponse{Content: "42", Provider: "anthropic", Model: "claude-x", RequestID: "req-echo"}}
	fx := setupProcessJobFixture(t, llm)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("what is the answer", agentID)
	if err := fx.taskRepo.Insert(ctx, task); err != nil {
		t.Fatalf("insert task failed: %v", err)
	}

	enqueued, err := fx.enqueue.Enqueue(ctx, task.ID)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if enqueued.Status != domain.ExecutionPending {
		t.Fatalf("expected PENDING right after enqueue, got %s", enqueued.Status)
	}

	execution, err := fx.process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if execution.Status != domain.ExecutionCompleted {
		t.Fatalf("expected COMPLETED, got %+v", execution)
	}
	if execution.Result == nil || *execution.Result != "42" {
		t.Fatalf("unexpected result: %+v", execution.Result)
	}
	if execution.ContextSnapshotID == nil {
		t.Fatal("expected context snapshot id attached")
	}

	finalTask, err := fx.taskRepo.FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task failed: %v", err)
	}
	if finalTask.Status != domain.TaskCompleted {
		t.Fatalf("expected task COMPLETED, got %s", finalTask.Status)
	}
}

func TestProcessJobUseCase_GatewayFailure_PreservesSnapshot(t *testing.T) {
	llm := fakeLlmGateway{err: &gateway.CallError{Status: 504, Message: "Gateway did not respond in time"}}
	fx := setupProcessJobFixture(t, llm)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("do the thing", agentID)
	_ = fx.taskRepo.Insert(ctx, task)

	enqueued, err := fx.enqueue.Enqueue(ctx, task.ID)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	execution, err := fx.process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("expected a swallowed error turned into a FAILED execution, got hard error: %v", err)
	}
	if execution.Status != domain.ExecutionFailed {
		t.Fatalf("expected FAILED, got %+v", execution)
	}
	if execution.ContextSnapshotID == nil {
		t.Fatal("expected context snapshot id preserved even though the gateway call failed")
	}

	finalTask, err := fx.taskRepo.FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task failed: %v", err)
	}
	if finalTask.Status != domain.TaskFailed {
		t.Fatalf("expected task FAILED, got %s", finalTask.Status)
	}
}

func TestProcessJobUseCase_Idempotent_AlreadyCompleted(t *testing.T) {
	llm := fakeLlmGateway{resp: gateway.ChatResponse{Content: "once", Provider: "anthropic", Model: "claude-x", RequestID: "req-once"}}
	fx := setupProcessJobFixture(t, llm)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("idempotency check", agentID)
	_ = fx.taskRepo.Insert(ctx, task)
	enqueued, _ := fx.enqueue.Enqueue(ctx, task.ID)

	first, err := fx.process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("first process failed: %v", err)
	}
	if first.Status != domain.ExecutionCompleted {
		t.Fatalf("expected COMPLETED, got %s", first.Status)
	}

	// Simulate a redelivered/duplicate wake for the same job after it already completed — the
	// job is DONE and no longer claimable, so this must short-circuit on the terminal
	// Execution rather than re-entering the claim/gateway path.
	second, err := fx.process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("second process call errored: %v", err)
	}
	if second.Status != domain.ExecutionCompleted || second.Result == nil || *second.Result != "once" {
		t.Fatalf("expected idempotent re-read of the completed execution, got %+v", second)
	}
}

func TestProcessJobUseCase_JobNotFound(t *testing.T) {
	fx := setupProcessJobFixture(t, fakeLlmGateway{})
	ctx := context.Background()

	_, err := fx.process.ProcessExecution(ctx, domain.NewExecutionID())
	if !errors.Is(err, application.ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound for a job that was never enqueued, got %v", err)
	}
}

func TestProcessJobUseCase_UnknownAgent(t *testing.T) {
	fx := setupProcessJobFixture(t, fakeLlmGateway{})
	ctx := context.Background()

	ghostAgentID, _ := domain.NewAgentID("ghost")
	// Bypass CreateTaskUseCase's own agent validation to simulate a catalog change after task
	// creation, per PreloOrchestrator.resolveAgent's own comment about this exact scenario.
	task, _ := domain.NewTask("do the thing", ghostAgentID)
	_ = fx.taskRepo.Insert(ctx, task)

	enqueued, err := fx.enqueue.Enqueue(ctx, task.ID)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	execution, err := fx.process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("expected a swallowed unknown-agent error turned into a FAILED execution, got hard error: %v", err)
	}
	if execution.Status != domain.ExecutionFailed {
		t.Fatalf("expected FAILED, got %+v", execution)
	}
}

// TestProcessJobUseCase_GatewayFailure_SanitizesTheErrorMessage is the regression test for a
// code-review finding: Execution.Error is served back unauthenticated via
// GET /tasks/{id}/executions/{id} (prelo-core has no auth today), so the raw wrapped transport
// error (which can include internal hostnames, DNS details, etc.) must never end up there —
// only *gateway.CallError's own fixed, non-leaking Message should.
func TestProcessJobUseCase_GatewayFailure_SanitizesTheErrorMessage(t *testing.T) {
	rawDetail := "dial tcp internal-llm-gateway.private:8081: connect: connection refused"
	llm := fakeLlmGateway{err: &gateway.CallError{Status: 502, Message: "Gateway communication failed", Err: errors.New(rawDetail)}}
	fx := setupProcessJobFixture(t, llm)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("do the thing", agentID)
	_ = fx.taskRepo.Insert(ctx, task)
	enqueued, err := fx.enqueue.Enqueue(ctx, task.ID)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	execution, err := fx.process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if execution.Error == nil {
		t.Fatal("expected an error message")
	}
	if strings.Contains(*execution.Error, rawDetail) {
		t.Fatalf("expected the raw transport detail to be stripped, got %q", *execution.Error)
	}
	if *execution.Error != "Gateway communication failed" {
		t.Fatalf("expected the sanitized CallError.Message, got %q", *execution.Error)
	}
}
