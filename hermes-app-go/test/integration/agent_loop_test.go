package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/agentregistry"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/gateway"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/persistence/postgres"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/toolregistry"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/tools"
)

// toolAwareLlmGateway mirrors MockProvider.java's own convention exactly (see its doc
// comment): asks for targetTool once per execution, then answers FINAL as soon as it sees a
// "[tool_result:" message — so these tests exercise the real loop logic the same way the real
// Gateway (in mock mode) would, without needing an HTTP round trip to it.
type toolAwareLlmGateway struct {
	calls        int
	targetTool   string
	toolArgsJSON string // defaults to "{}" when empty
}

func (g *toolAwareLlmGateway) Chat(_ context.Context, req gateway.ChatRequest, requestID string) (gateway.ChatResponse, error) {
	g.calls++
	last := req.Messages[len(req.Messages)-1]
	if len(req.Tools) > 0 && g.targetTool != "" && !strings.HasPrefix(last.Content, "[tool_result:") {
		args := g.toolArgsJSON
		if args == "" {
			args = "{}"
		}
		return gateway.ChatResponse{Kind: gateway.KindToolUse, ToolName: g.targetTool, ToolArgsJSON: args, RequestID: requestID}, nil
	}
	return gateway.ChatResponse{Kind: gateway.KindFinal, Content: "done", Provider: "mock", Model: "mock-echo", RequestID: requestID}, nil
}

type agentLoopFixture struct {
	taskRepo     *postgres.TaskRepository
	execRepo     *postgres.ExecutionRepository
	jobRepo      *postgres.ExecutionJobRepository
	toolCallRepo *postgres.ToolCallRepository
	approvalRepo *postgres.ApprovalRepository
	turnRepo     *postgres.ExecutionTurnRepository
	enqueue      *application.EnqueueExecutionUseCase
	process      *application.ProcessJobUseCase
	decide       *application.DecideApprovalUseCase
	llm          *toolAwareLlmGateway
}

func setupAgentLoopFixture(t *testing.T, targetTool string) agentLoopFixture {
	t.Helper()
	pool := newTestPool(t)

	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	jobRepo := postgres.NewExecutionJobRepository(pool)
	manualRepo := postgres.NewManualContextRepository(pool)
	snapshotRepo := postgres.NewContextSnapshotRepository(pool)
	resolver := application.NewContextResolver(manualRepo, snapshotRepo)
	toolCallRepo := postgres.NewToolCallRepository(pool)
	approvalRepo := postgres.NewApprovalRepository(pool)
	turnRepo := postgres.NewExecutionTurnRepository(pool)
	suspensionRepo := postgres.NewExecutionSuspensionRepository(pool)

	agents, err := agentregistry.LoadDefault()
	if err != nil {
		t.Fatalf("failed to load agent catalog: %v", err)
	}

	registry := toolregistry.NewStatic(tools.NewCurrentTimeTool(), tools.NewEchoTool())
	llm := &toolAwareLlmGateway{targetTool: targetTool}
	invokeTool := application.NewInvokeToolUseCase(execRepo, agents, registry, application.NewDefaultPermissionPolicy(), toolCallRepo, approvalRepo)
	loop := application.NewRunAgentLoopUseCase(llm, registry, invokeTool, turnRepo, suspensionRepo, jobRepo)
	process := application.NewProcessJobUseCase(taskRepo, execRepo, jobRepo, agents, resolver, snapshotRepo, loop, turnRepo, suspensionRepo, "loop-test-worker")
	decide := application.NewDecideApprovalUseCase(approvalRepo, toolCallRepo, registry, execRepo, turnRepo, suspensionRepo, jobRepo)
	enqueue := application.NewEnqueueExecutionUseCase(taskRepo, execRepo, jobRepo, noopPublisher{})

	return agentLoopFixture{
		taskRepo: taskRepo, execRepo: execRepo, jobRepo: jobRepo, toolCallRepo: toolCallRepo,
		approvalRepo: approvalRepo, turnRepo: turnRepo, enqueue: enqueue, process: process, decide: decide, llm: llm,
	}
}

// TestAgentLoop_LowRiskTool_CompletesAutomatically proves the whole point of Fase B: the LLM
// asks for a LOW-risk tool it holds the capability for, the loop runs it without any human
// involved, feeds the result back, and the execution completes — three ledger turns
// (LLM_CALL asking for the tool, TOOL_CALL executed, LLM_CALL answering FINAL), no suspension.
func TestAgentLoop_LowRiskTool_CompletesAutomatically(t *testing.T) {
	fx := setupAgentLoopFixture(t, "current_time")
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("what time is it?", agentID)
	if err := fx.taskRepo.Insert(ctx, task); err != nil {
		t.Fatalf("insert task failed: %v", err)
	}
	enqueued, err := fx.enqueue.Enqueue(ctx, task.ID)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	execution, err := fx.process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if execution.Status != domain.ExecutionCompleted {
		t.Fatalf("expected COMPLETED, got %+v", execution)
	}
	if execution.Result == nil || *execution.Result != "done" {
		t.Fatalf("unexpected result: %+v", execution.Result)
	}
	if fx.llm.calls != 2 {
		t.Fatalf("expected exactly 2 Gateway calls (ask for tool, then final), got %d", fx.llm.calls)
	}

	turns, err := fx.turnRepo.ListByExecution(ctx, execution.ID)
	if err != nil {
		t.Fatalf("list turns failed: %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("expected 3 ledger turns, got %d: %+v", len(turns), turns)
	}
	if turns[0].Kind != domain.TurnLLMCall || turns[1].Kind != domain.TurnToolCall || turns[2].Kind != domain.TurnLLMCall {
		t.Fatalf("unexpected turn sequence: %+v", turns)
	}
	for _, turn := range turns {
		if turn.CompletedAt == nil {
			t.Fatalf("expected every turn to be resolved on a completed execution, got %+v", turn)
		}
	}
}

// TestAgentLoop_ModerateRiskTool_SuspendsThenApprovalResumes proves the suspend/resume half of
// Fase B end to end: the loop stops cold (no further Gateway calls) the moment it hits a
// REQUIRE_APPROVAL tool, the job sits AWAITING_RESUME, and only after a human approves does the
// job become processable again — reprocessing it (simulating the sweeper's next tick) replays
// from the ledger and reaches FINAL, without ever re-asking the first turn.
func TestAgentLoop_ModerateRiskTool_SuspendsThenApprovalResumes(t *testing.T) {
	fx := setupAgentLoopFixture(t, "echo")
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("echo something", agentID)
	if err := fx.taskRepo.Insert(ctx, task); err != nil {
		t.Fatalf("insert task failed: %v", err)
	}
	enqueued, err := fx.enqueue.Enqueue(ctx, task.ID)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	suspended, err := fx.process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if suspended.Status != domain.ExecutionRunning {
		t.Fatalf("expected the Execution to stay RUNNING while suspended, got %s", suspended.Status)
	}
	if fx.llm.calls != 1 {
		t.Fatalf("expected exactly 1 Gateway call before suspending, got %d", fx.llm.calls)
	}

	job, err := fx.jobRepo.FindByID(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("find job failed: %v", err)
	}
	if job.Status != domain.JobAwaitingResume {
		t.Fatalf("expected AWAITING_RESUME, got %s", job.Status)
	}

	pending, err := fx.approvalRepo.ListPending(ctx)
	if err != nil {
		t.Fatalf("list pending approvals failed: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected exactly 1 pending approval, got %d", len(pending))
	}

	if _, err := fx.decide.Approve(ctx, pending[0].ID, "tester"); err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	resumedJob, err := fx.jobRepo.FindByID(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("find job failed: %v", err)
	}
	if resumedJob.Status != domain.JobPending {
		t.Fatalf("expected PENDING after approval, got %s", resumedJob.Status)
	}

	// Simulate the sweeper's next tick picking the resumed job back up.
	final, err := fx.process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("resume processing failed: %v", err)
	}
	if final.Status != domain.ExecutionCompleted {
		t.Fatalf("expected COMPLETED after resume, got %+v", final)
	}
	if final.Result == nil || *final.Result != "done" {
		t.Fatalf("unexpected result: %+v", final.Result)
	}
	// Only one more Gateway call across the whole approve+resume cycle — proves the loop did
	// not redo the first (already-completed) turn.
	if fx.llm.calls != 2 {
		t.Fatalf("expected exactly 2 total Gateway calls across suspend+resume, got %d", fx.llm.calls)
	}

	turns, err := fx.turnRepo.ListByExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("list turns failed: %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("expected 3 ledger turns, got %d: %+v", len(turns), turns)
	}
	for _, turn := range turns {
		if turn.CompletedAt == nil {
			t.Fatalf("expected every turn to be resolved once the execution completed, got %+v", turn)
		}
	}

	finalJob, err := fx.jobRepo.FindByID(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("find job failed: %v", err)
	}
	if finalJob.Status != domain.JobDone {
		t.Fatalf("expected job DONE, got %s", finalJob.Status)
	}
}

// TestAgentLoop_OrphanedTurnFailsInsteadOfSilentlyDuplicating is the regression test for a
// code-review finding: a turn left with neither Output nor Error (as a crash between the
// Gateway actually responding and that response being persisted would leave it) used to be
// silently skipped by the replay logic, dropping its content from context and letting the loop
// issue a brand-new Gateway call at the next turn number — a real duplicate call the whole
// ledger/idempotency design exists to prevent. It must now fail the execution instead of
// guessing.
func TestAgentLoop_OrphanedTurnFailsInsteadOfSilentlyDuplicating(t *testing.T) {
	fx := setupAgentLoopFixture(t, "current_time")
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("crash recovery test", agentID)
	if err := fx.taskRepo.Insert(ctx, task); err != nil {
		t.Fatalf("insert task failed: %v", err)
	}
	enqueued, err := fx.enqueue.Enqueue(ctx, task.ID)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Simulate a crash exactly between "the Gateway responded" and "the turn's outcome was
	// persisted": a turn 0 row exists with no Output and no Error at all.
	orphaned := domain.NewExecutionTurn(enqueued.ID, 0, domain.TurnLLMCall, "prompt")
	if _, err := fx.turnRepo.Insert(ctx, orphaned); err != nil {
		t.Fatalf("insert orphaned turn failed: %v", err)
	}

	result, err := fx.process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if result.Status != domain.ExecutionFailed {
		t.Fatalf("expected FAILED, got %+v", result)
	}
	if result.Error == nil || !strings.Contains(*result.Error, "unresolved turn") {
		t.Fatalf("expected the failure to mention the unresolved turn, got %v", result.Error)
	}
	if fx.llm.calls != 0 {
		t.Fatalf("expected zero Gateway calls — the loop must refuse to guess, not risk a duplicate call, got %d", fx.llm.calls)
	}
}
