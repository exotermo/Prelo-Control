package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/persistence/postgres"
)

func setupTaskAndExecution(t *testing.T, taskRepo *postgres.TaskRepository, execRepo *postgres.ExecutionRepository) (domain.Task, domain.Execution) {
	t.Helper()
	ctx := context.Background()
	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("ledger test", agentID)
	if err := taskRepo.Insert(ctx, task); err != nil {
		t.Fatalf("insert task failed: %v", err)
	}
	exec := domain.NewPendingExecution(task.ID, agentID)
	if err := execRepo.Insert(ctx, exec); err != nil {
		t.Fatalf("insert execution failed: %v", err)
	}
	return task, exec
}

func TestExecutionTurnRepository_InsertAndListInOrder(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	turnRepo := postgres.NewExecutionTurnRepository(pool)

	_, exec := setupTaskAndExecution(t, taskRepo, execRepo)

	turn0 := domain.NewExecutionTurn(exec.ID, 0, domain.TurnLLMCall, "system+user prompt")
	if _, err := turnRepo.Insert(ctx, turn0); err != nil {
		t.Fatalf("insert turn 0 failed: %v", err)
	}
	turn1 := domain.NewExecutionTurn(exec.ID, 1, domain.TurnToolCall, "current_time({})")
	if _, err := turnRepo.Insert(ctx, turn1.Completed("2026-01-01T00:00:00Z")); err != nil {
		t.Fatalf("insert turn 1 failed: %v", err)
	}

	turns, err := turnRepo.ListByExecution(ctx, exec.ID)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if turns[0].TurnNumber != 0 || turns[1].TurnNumber != 1 {
		t.Fatalf("expected turns ordered by turn_number, got %+v", turns)
	}
	if turns[1].Output == nil || *turns[1].Output != "2026-01-01T00:00:00Z" {
		t.Fatalf("expected turn 1 output to be persisted, got %+v", turns[1])
	}
}

// TestExecutionTurnRepository_DuplicateTurnNumberIsRejected proves the idempotency guarantee
// the whole ledger design depends on: a worker that crashes and gets its job reprocessed by
// the sweeper must not be able to double-insert the same (execution_id, turn_number) — the
// unique index, not application logic, is the actual backstop.
func TestExecutionTurnRepository_DuplicateTurnNumberIsRejected(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	turnRepo := postgres.NewExecutionTurnRepository(pool)

	_, exec := setupTaskAndExecution(t, taskRepo, execRepo)

	turn := domain.NewExecutionTurn(exec.ID, 0, domain.TurnLLMCall, "prompt")
	if _, err := turnRepo.Insert(ctx, turn); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Same execution+turn number, freshly generated ExecutionTurnID (simulating a naive retry
	// that forgot to check FindByRequestID first) — must fail, not silently duplicate.
	retry := domain.NewExecutionTurn(exec.ID, 0, domain.TurnLLMCall, "prompt")
	if _, err := turnRepo.Insert(ctx, retry); err == nil {
		t.Fatal("expected a duplicate (execution_id, turn_number) insert to fail")
	}
}

func TestExecutionTurnRepository_FindByRequestIDIsTheIdempotencyCheck(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	turnRepo := postgres.NewExecutionTurnRepository(pool)

	_, exec := setupTaskAndExecution(t, taskRepo, execRepo)

	requestID := domain.TurnRequestID(exec.ID, 3)
	if _, found, err := turnRepo.FindByRequestID(ctx, requestID); err != nil || found {
		t.Fatalf("expected not found before insert, got found=%v err=%v", found, err)
	}

	turn := domain.NewExecutionTurn(exec.ID, 3, domain.TurnLLMCall, "prompt")
	if turn.RequestID != requestID {
		t.Fatalf("expected deterministic RequestID %q, got %q", requestID, turn.RequestID)
	}
	if _, err := turnRepo.Insert(ctx, turn); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	found, ok, err := turnRepo.FindByRequestID(ctx, requestID)
	if err != nil || !ok {
		t.Fatalf("expected found=true after insert, got found=%v err=%v", ok, err)
	}
	if found.TurnNumber != 3 {
		t.Fatalf("unexpected turn: %+v", found)
	}
}

func TestExecutionSuspensionRepository_InsertFindResolve(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	suspensionRepo := postgres.NewExecutionSuspensionRepository(pool)

	_, exec := setupTaskAndExecution(t, taskRepo, execRepo)

	suspension := domain.NewExecutionSuspension(exec.ID, domain.SuspensionApproval, "approval-123")
	if err := suspensionRepo.Insert(ctx, suspension); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	found, ok, err := suspensionRepo.FindActiveByResumeKey(ctx, domain.SuspensionApproval, "approval-123")
	if err != nil || !ok {
		t.Fatalf("expected an active suspension, got ok=%v err=%v", ok, err)
	}
	if found.ExecutionID != exec.ID {
		t.Fatalf("unexpected suspension: %+v", found)
	}

	if err := suspensionRepo.Resolve(ctx, found.ID); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	_, ok, err = suspensionRepo.FindActiveByResumeKey(ctx, domain.SuspensionApproval, "approval-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected the suspension to no longer be active after Resolve")
	}
}

// TestExecutionJobRepository_SuspendAndResume proves the job-status half of the Fase A
// mechanism: a RUNNING job can be suspended (no lease left behind) and only Resume brings it
// back to PENDING, claimable again.
func TestExecutionJobRepository_SuspendAndResume(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	jobRepo := postgres.NewExecutionJobRepository(pool)

	_, exec := setupTaskAndExecution(t, taskRepo, execRepo)
	if err := jobRepo.Insert(ctx, domain.NewExecutionJob(exec.TaskID, exec.ID)); err != nil {
		t.Fatalf("insert job failed: %v", err)
	}
	if _, err := jobRepo.Claim(ctx, exec.ID, "worker-1", time.Minute); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if err := jobRepo.MarkRunning(ctx, exec.ID, time.Minute); err != nil {
		t.Fatalf("mark running failed: %v", err)
	}

	if err := jobRepo.Suspend(ctx, exec.ID); err != nil {
		t.Fatalf("suspend failed: %v", err)
	}
	suspended, err := jobRepo.FindByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if suspended.Status != domain.JobAwaitingResume {
		t.Fatalf("expected AWAITING_RESUME, got %s", suspended.Status)
	}
	if suspended.LeaseExpiresAt != nil {
		t.Fatalf("expected no lease on a suspended job, got %+v", suspended.LeaseExpiresAt)
	}

	// A suspended job must not be claimable — it isn't PENDING/RETRY.
	if _, err := jobRepo.Claim(ctx, exec.ID, "worker-2", time.Minute); !isClaimLost(err) {
		t.Fatalf("expected ErrJobClaimLost claiming a suspended job, got %v", err)
	}

	if err := jobRepo.Resume(ctx, exec.ID); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	resumed, err := jobRepo.FindByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if resumed.Status != domain.JobPending {
		t.Fatalf("expected PENDING after resume, got %s", resumed.Status)
	}

	if _, err := jobRepo.Claim(ctx, exec.ID, "worker-2", time.Minute); err != nil {
		t.Fatalf("expected the resumed job to be claimable again: %v", err)
	}
}

// TestSweeper_NeverTouchesAnAwaitingResumeJob is the load-bearing invariant Fase A depends on:
// a job legitimately waiting on a human (or a sub-task) must survive the sweeper's orphan
// sweep indefinitely, even though it has no active lease — RequeueOrphaned's own WHERE clause
// (status IN ('CLAIMED','RUNNING')) is what guarantees this, not a special case for
// AWAITING_RESUME; this test is what would catch someone "fixing" that clause into something
// broader later.
func TestSweeper_NeverTouchesAnAwaitingResumeJob(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	jobRepo := postgres.NewExecutionJobRepository(pool)

	_, exec := setupTaskAndExecution(t, taskRepo, execRepo)
	if err := jobRepo.Insert(ctx, domain.NewExecutionJob(exec.TaskID, exec.ID)); err != nil {
		t.Fatalf("insert job failed: %v", err)
	}
	if _, err := jobRepo.Claim(ctx, exec.ID, "worker-1", time.Minute); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if err := jobRepo.MarkRunning(ctx, exec.ID, time.Minute); err != nil {
		t.Fatalf("mark running failed: %v", err)
	}
	if err := jobRepo.Suspend(ctx, exec.ID); err != nil {
		t.Fatalf("suspend failed: %v", err)
	}

	retried, err := jobRepo.RequeueOrphaned(ctx)
	if err != nil {
		t.Fatalf("requeue orphaned failed: %v", err)
	}
	if len(retried) != 0 {
		t.Fatalf("expected the sweeper to leave the suspended job alone, got %v", retried)
	}

	still, err := jobRepo.FindByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if still.Status != domain.JobAwaitingResume {
		t.Fatalf("expected AWAITING_RESUME to survive the sweeper untouched, got %s", still.Status)
	}
}

func isClaimLost(err error) bool {
	return errors.Is(err, application.ErrJobClaimLost)
}
