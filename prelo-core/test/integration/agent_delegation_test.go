package integration

import (
	"context"
	"testing"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/agentregistry"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
	"github.com/exotermo/prelo-core/internal/infrastructure/toolregistry"
	"github.com/exotermo/prelo-core/internal/infrastructure/tools"
)

type delegationFixture struct {
	taskRepo     *postgres.TaskRepository
	execRepo     *postgres.ExecutionRepository
	jobRepo      *postgres.ExecutionJobRepository
	approvalRepo *postgres.ApprovalRepository
	turnRepo     *postgres.ExecutionTurnRepository
	enqueue      *application.EnqueueExecutionUseCase
	process      *application.ProcessJobUseCase
	decide       *application.DecideApprovalUseCase
	llm          *toolAwareLlmGateway
}

func setupDelegationFixture(t *testing.T) delegationFixture {
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

	enqueue := application.NewEnqueueExecutionUseCase(taskRepo, execRepo, jobRepo, noopPublisher{})
	registry := toolregistry.NewStatic(tools.NewCurrentTimeTool(), tools.NewEchoTool(), tools.NewDelegateTool(taskRepo, agents, enqueue))
	llm := &toolAwareLlmGateway{targetTool: domain.DelegateToolName, toolArgsJSON: `{"agentId":"general","description":"do the delegated sub-work"}`}
	invokeTool := application.NewInvokeToolUseCase(execRepo, agents, registry, application.NewDefaultPermissionPolicy(), toolCallRepo, approvalRepo)
	loop := application.NewRunAgentLoopUseCase(llm, registry, invokeTool, turnRepo, suspensionRepo, jobRepo)
	process := application.NewProcessJobUseCase(taskRepo, execRepo, jobRepo, agents, resolver, snapshotRepo, loop, turnRepo, suspensionRepo, "delegation-test-worker")
	decide := application.NewDecideApprovalUseCase(approvalRepo, toolCallRepo, registry, execRepo, turnRepo, suspensionRepo, jobRepo)

	return delegationFixture{
		taskRepo: taskRepo, execRepo: execRepo, jobRepo: jobRepo, approvalRepo: approvalRepo,
		turnRepo: turnRepo, enqueue: enqueue, process: process, decide: decide, llm: llm,
	}
}

// TestDelegation_ParentSuspendsUntilChildCompletesThenResumes is Fase C's end-to-end proof: the
// LLM asks to delegate, which (being MODERATE by default) needs approval; approving it creates
// and enqueues a real child Task instead of running anything inline; the parent stays suspended
// — now on the CHILD, not the approval — until that child's own execution actually finishes;
// only then does ProcessJobUseCase's completion hook feed the child's result back and resume
// the parent, which reaches FINAL exactly like any other tool result would.
func TestDelegation_ParentSuspendsUntilChildCompletesThenResumes(t *testing.T) {
	fx := setupDelegationFixture(t)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	parentTask, _ := domain.NewTask("delegate this to another agent", agentID)
	if err := fx.taskRepo.Insert(ctx, parentTask); err != nil {
		t.Fatalf("insert parent task failed: %v", err)
	}
	enqueuedParent, err := fx.enqueue.Enqueue(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("enqueue parent failed: %v", err)
	}

	suspended, err := fx.process.ProcessExecution(ctx, enqueuedParent.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if suspended.Status != domain.ExecutionRunning {
		t.Fatalf("expected the parent Execution to stay RUNNING while suspended, got %s", suspended.Status)
	}

	pending, err := fx.approvalRepo.ListPending(ctx)
	if err != nil {
		t.Fatalf("list pending approvals failed: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected exactly 1 pending approval for the delegation, got %d", len(pending))
	}

	if _, err := fx.decide.Approve(ctx, pending[0].ID, "tester"); err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	// Approving must NOT resume the parent job yet — it's now waiting on the child, not on the
	// (already-decided) approval.
	parentJob, err := fx.jobRepo.FindByID(ctx, enqueuedParent.ID)
	if err != nil {
		t.Fatalf("find parent job failed: %v", err)
	}
	if parentJob.Status != domain.JobAwaitingResume {
		t.Fatalf("expected the parent job to still be AWAITING_RESUME (now on the child), got %s", parentJob.Status)
	}

	children, err := fx.taskRepo.FindChildren(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("find children failed: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected exactly 1 child task, got %d", len(children))
	}
	child := children[0]
	if child.ParentTaskID == nil || *child.ParentTaskID != parentTask.ID {
		t.Fatalf("expected the child to point back at the parent, got %+v", child.ParentTaskID)
	}
	if child.Depth != 1 {
		t.Fatalf("expected the child's depth to be 1, got %d", child.Depth)
	}

	childExecution, err := fx.execRepo.FindByTaskID(ctx, child.ID)
	if err != nil {
		t.Fatalf("find child execution failed: %v", err)
	}
	if childExecution.Status != domain.ExecutionPending {
		t.Fatalf("expected the child execution to be PENDING, got %s", childExecution.Status)
	}

	// The mock gateway always answers FINAL once it stops seeing a fresh tool request — the
	// child's own task never asked for a tool (targetTool was only set for the parent's ask),
	// so processing it completes immediately, same as any plain task.
	fx.llm.targetTool = ""
	finishedChild, err := fx.process.ProcessExecution(ctx, childExecution.ID)
	if err != nil {
		t.Fatalf("process child execution failed: %v", err)
	}
	if finishedChild.Status != domain.ExecutionCompleted {
		t.Fatalf("expected the child execution to complete, got %+v", finishedChild)
	}

	// Processing the child must have triggered the parent's resume automatically (the
	// completion hook), without anything else calling jobs.Resume by hand.
	parentJobAfterChild, err := fx.jobRepo.FindByID(ctx, enqueuedParent.ID)
	if err != nil {
		t.Fatalf("find parent job failed: %v", err)
	}
	if parentJobAfterChild.Status != domain.JobPending {
		t.Fatalf("expected the parent job to be PENDING again after the child completed, got %s", parentJobAfterChild.Status)
	}

	finishedParent, err := fx.process.ProcessExecution(ctx, enqueuedParent.ID)
	if err != nil {
		t.Fatalf("resume parent processing failed: %v", err)
	}
	if finishedParent.Status != domain.ExecutionCompleted {
		t.Fatalf("expected the parent execution to complete, got %+v", finishedParent)
	}
	if finishedParent.Result == nil {
		t.Fatal("expected the parent's final result to be set")
	}

	parentTurns, err := fx.turnRepo.ListByExecution(ctx, enqueuedParent.ID)
	if err != nil {
		t.Fatalf("list parent turns failed: %v", err)
	}
	for _, turn := range parentTurns {
		if turn.CompletedAt == nil {
			t.Fatalf("expected every parent turn to be resolved once the parent completed, got %+v", turn)
		}
	}
}
