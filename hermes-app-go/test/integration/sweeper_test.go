package integration

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/agentregistry"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/gateway"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/persistence/postgres"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/toolregistry"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/worker"
)

// buildProcessJob wires a ProcessJobUseCase with an empty tool registry (Fase B's agent loop
// needs one even when a test never exercises tool-calling at all).
func buildProcessJob(pool *pgxpool.Pool, taskRepo *postgres.TaskRepository, execRepo *postgres.ExecutionRepository, jobRepo *postgres.ExecutionJobRepository, resolver *application.ContextResolver, snapshotRepo *postgres.ContextSnapshotRepository, agents application.AgentRegistry, llm application.LanguageModelGateway, workerID string) *application.ProcessJobUseCase {
	toolCallRepo := postgres.NewToolCallRepository(pool)
	approvalRepo := postgres.NewApprovalRepository(pool)
	turnRepo := postgres.NewExecutionTurnRepository(pool)
	suspensionRepo := postgres.NewExecutionSuspensionRepository(pool)
	emptyTools := toolregistry.NewStatic()
	invokeTool := application.NewInvokeToolUseCase(execRepo, agents, emptyTools, application.NewDefaultPermissionPolicy(), toolCallRepo, approvalRepo)
	agentLoop := application.NewRunAgentLoopUseCase(llm, emptyTools, invokeTool, turnRepo, suspensionRepo, jobRepo)
	return application.NewProcessJobUseCase(taskRepo, execRepo, jobRepo, agents, resolver, snapshotRepo, agentLoop, turnRepo, suspensionRepo, workerID)
}

type countingLlmGateway struct {
	calls int64
}

func (g *countingLlmGateway) Chat(context.Context, gateway.ChatRequest, string) (gateway.ChatResponse, error) {
	atomic.AddInt64(&g.calls, 1)
	return gateway.ChatResponse{Content: "recovered", Provider: "anthropic", Model: "claude-x", RequestID: "req-recovered"}, nil
}

// TestSweeper_RequeuesOrphanedJobAndProcessesExactlyOnce is the etapa G11 acceptance test: a
// job left CLAIMED with an expired lease (simulating a worker that crashed mid-job) must be
// requeued by the sweeper and reprocessed to completion exactly once — asserted both by the
// final Execution state and by counting real Gateway calls.
func TestSweeper_RequeuesOrphanedJobAndProcessesExactlyOnce(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

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

	llm := &countingLlmGateway{}
	enqueue := application.NewEnqueueExecutionUseCase(taskRepo, execRepo, jobRepo, noopPublisher{})
	process := buildProcessJob(pool, taskRepo, execRepo, jobRepo, resolver, snapshotRepo, agents, llm, "crashed-worker")

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("recover me", agentID)
	if err := taskRepo.Insert(ctx, task); err != nil {
		t.Fatalf("insert task failed: %v", err)
	}
	enqueued, err := enqueue.Enqueue(ctx, task.ID)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Simulate a worker that claimed the job, crashed before doing anything else: claim it
	// with an already-expired lease (negative duration), matching what Claim would have
	// looked like moments before a crash whose lease has since run out.
	if _, err := jobRepo.Claim(ctx, enqueued.ID, "crashed-worker", -time.Minute); err != nil {
		t.Fatalf("simulated claim failed: %v", err)
	}
	orphaned, err := jobRepo.FindByID(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("find job failed: %v", err)
	}
	if orphaned.Status != domain.JobClaimed {
		t.Fatalf("expected simulated job to be CLAIMED, got %s", orphaned.Status)
	}

	sweeper := worker.NewSweeper(jobRepo, process, time.Hour /* never auto-ticks in this test */, 20)

	// First sweep: requeues the orphaned CLAIMED job to RETRY (with a backoff window) but
	// does NOT process it yet, since available_at is now in the future.
	requeued, err := jobRepo.RequeueOrphaned(ctx)
	if err != nil {
		t.Fatalf("requeue orphaned failed: %v", err)
	}
	if len(requeued) != 1 || requeued[0] != enqueued.ID {
		t.Fatalf("expected exactly the orphaned job to be requeued, got %v", requeued)
	}
	afterRequeue, err := jobRepo.FindByID(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("find job failed: %v", err)
	}
	if afterRequeue.Status != domain.JobRetry || afterRequeue.Attempt != 1 {
		t.Fatalf("expected RETRY with attempt=1, got %+v", afterRequeue)
	}

	// Force the backoff window open (as if enough time had passed) and let a manual sweep
	// tick claim + process it — this exercises the exact same path Sweeper.Run's ticker would.
	if _, err := pool.Exec(ctx, `UPDATE execution_jobs SET available_at = now() - interval '1 second' WHERE id = $1`, enqueued.ID.Value); err != nil {
		t.Fatalf("failed to force job claimable: %v", err)
	}

	claimable, err := jobRepo.ListClaimable(ctx, 20)
	if err != nil {
		t.Fatalf("list claimable failed: %v", err)
	}
	found := false
	for _, id := range claimable {
		if id == enqueued.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the requeued job to be claimable, got %v", claimable)
	}

	execution, err := process.ProcessExecution(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("recovery processing failed: %v", err)
	}
	if execution.Status != domain.ExecutionCompleted {
		t.Fatalf("expected COMPLETED after recovery, got %+v", execution)
	}
	if execution.Result == nil || *execution.Result != "recovered" {
		t.Fatalf("unexpected result: %+v", execution.Result)
	}

	if calls := atomic.LoadInt64(&llm.calls); calls != 1 {
		t.Fatalf("expected exactly 1 real Gateway call across the whole crash+recovery cycle, got %d", calls)
	}

	finalJob, err := jobRepo.FindByID(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("find job failed: %v", err)
	}
	if finalJob.Status != domain.JobDone {
		t.Fatalf("expected job DONE, got %s", finalJob.Status)
	}
	_ = sweeper
}

// TestSweeper_Run_EndToEnd exercises Sweeper.Run itself (not just its two building blocks
// called manually) against a job that starts out immediately claimable — the ticking sweeper,
// with no Redis wake at all, must still drive it to completion, proving the sweeper alone is
// a full fallback delivery path.
func TestSweeper_Run_EndToEnd(t *testing.T) {
	pool := newTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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

	llm := &countingLlmGateway{}
	enqueue := application.NewEnqueueExecutionUseCase(taskRepo, execRepo, jobRepo, noopPublisher{})
	process := buildProcessJob(pool, taskRepo, execRepo, jobRepo, resolver, snapshotRepo, agents, llm, "sweeper-only")

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("no redis needed", agentID)
	_ = taskRepo.Insert(ctx, task)
	enqueued, err := enqueue.Enqueue(ctx, task.ID)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	sweeper := worker.NewSweeper(jobRepo, process, 200*time.Millisecond, 20)
	go sweeper.Run(ctx)

	deadline := time.After(4 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the sweeper-only path to complete the job")
		default:
		}
		exec, err := execRepo.FindByID(ctx, enqueued.ID)
		if err == nil && exec.Status == domain.ExecutionCompleted {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
