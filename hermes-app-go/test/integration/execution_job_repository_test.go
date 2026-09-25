package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/persistence/postgres"
)

func TestExecutionJobRepository_InsertClaimMarkDone(t *testing.T) {
	pool := newTestPool(t)
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	jobRepo := postgres.NewExecutionJobRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(ctx, task)
	exec := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(ctx, exec)

	job := domain.NewExecutionJob(task.ID, exec.ID)
	if err := jobRepo.Insert(ctx, job); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	claimed, err := jobRepo.Claim(ctx, exec.ID, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if claimed.Status != domain.JobClaimed || claimed.ClaimedBy == nil || *claimed.ClaimedBy != "worker-1" {
		t.Fatalf("unexpected claimed job: %+v", claimed)
	}

	if err := jobRepo.MarkRunning(ctx, exec.ID, time.Minute); err != nil {
		t.Fatalf("mark running failed: %v", err)
	}
	if err := jobRepo.MarkDone(ctx, exec.ID); err != nil {
		t.Fatalf("mark done failed: %v", err)
	}

	final, err := jobRepo.FindByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if final.Status != domain.JobDone {
		t.Fatalf("expected DONE, got %s", final.Status)
	}
}

func TestExecutionJobRepository_Claim_SecondAttemptLoses(t *testing.T) {
	pool := newTestPool(t)
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	jobRepo := postgres.NewExecutionJobRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(ctx, task)
	exec := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(ctx, exec)
	job := domain.NewExecutionJob(task.ID, exec.ID)
	_ = jobRepo.Insert(ctx, job)

	if _, err := jobRepo.Claim(ctx, exec.ID, "worker-1", time.Minute); err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if _, err := jobRepo.Claim(ctx, exec.ID, "worker-2", time.Minute); !errors.Is(err, application.ErrJobClaimLost) {
		t.Fatalf("expected ErrJobClaimLost, got %v", err)
	}
}

// TestExecutionJobRepository_ConcurrentClaim is the etapa G8 acceptance test: N goroutines
// racing to claim the same PENDING job row must yield exactly one winner.
func TestExecutionJobRepository_ConcurrentClaim(t *testing.T) {
	pool := newTestPool(t)
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	jobRepo := postgres.NewExecutionJobRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(ctx, task)
	exec := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(ctx, exec)
	job := domain.NewExecutionJob(task.ID, exec.ID)
	if err := jobRepo.Insert(ctx, job); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	const attempts = 10
	var wins int64
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		workerID := "worker-" + string(rune('a'+i))
		go func(workerID string) {
			defer wg.Done()
			if _, err := jobRepo.Claim(ctx, exec.ID, workerID, time.Minute); err == nil {
				atomic.AddInt64(&wins, 1)
			} else if !errors.Is(err, application.ErrJobClaimLost) {
				t.Errorf("unexpected error: %v", err)
			}
		}(workerID)
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("expected exactly 1 winning claim, got %d", wins)
	}

	final, err := jobRepo.FindByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if final.Status != domain.JobClaimed {
		t.Fatalf("expected final status CLAIMED, got %s", final.Status)
	}
}

// TestExecutionJobRepository_MarkRunningRenewsLeaseAndIsGuarded is the regression test for a
// code-review finding: MarkRunning used to only flip the status, leaving lease_expires_at
// exactly where Claim set it and never actually checking it still owned the job — a
// long-running multi-turn loop (Fase B) could outlive that first lease, and the sweeper would
// then hand the same job to a second worker while the first was still legitimately running.
func TestExecutionJobRepository_MarkRunningRenewsLeaseAndIsGuarded(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	jobRepo := postgres.NewExecutionJobRepository(pool)

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(ctx, task)
	exec := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(ctx, exec)
	_ = jobRepo.Insert(ctx, domain.NewExecutionJob(task.ID, exec.ID))

	// Claim with a short lease (as if the loop is about to run long past it), then MarkRunning
	// with a much longer one — the renewed lease, not the original, is what must end up stored.
	if _, err := jobRepo.Claim(ctx, exec.ID, "worker-1", time.Second); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	beforeRenewal, err := jobRepo.FindByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if err := jobRepo.MarkRunning(ctx, exec.ID, time.Hour); err != nil {
		t.Fatalf("mark running failed: %v", err)
	}
	renewed, err := jobRepo.FindByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if renewed.LeaseExpiresAt == nil || !renewed.LeaseExpiresAt.After(*beforeRenewal.LeaseExpiresAt) {
		t.Fatalf("expected MarkRunning to renew (extend) the lease, before=%v after=%v", beforeRenewal.LeaseExpiresAt, renewed.LeaseExpiresAt)
	}

	// A second MarkRunning call — as a "zombie" worker that already lost the job would issue
	// after the sweeper moved it away from CLAIMED/RUNNING — must fail instead of silently
	// succeeding. Simulate the loss directly: force the job back to PENDING (what Resume/a
	// sweeper reclaim would do) and confirm MarkRunning now refuses.
	if _, err := pool.Exec(ctx, `UPDATE execution_jobs SET status = 'PENDING' WHERE id = $1`, exec.ID.Value); err != nil {
		t.Fatalf("failed to force job PENDING: %v", err)
	}
	if err := jobRepo.MarkRunning(ctx, exec.ID, time.Hour); !errors.Is(err, application.ErrJobClaimLost) {
		t.Fatalf("expected ErrJobClaimLost when the job is no longer CLAIMED, got %v", err)
	}
}

// TestExecutionJobRepository_MarkDoneAndMarkFailedAreGuardedToRunning is the regression test for
// the other half of the same finding: a "zombie" worker whose job was already reclaimed (no
// longer RUNNING) must not be able to silently overwrite whatever the current owner is doing.
func TestExecutionJobRepository_MarkDoneAndMarkFailedAreGuardedToRunning(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	jobRepo := postgres.NewExecutionJobRepository(pool)

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(ctx, task)
	exec := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(ctx, exec)
	_ = jobRepo.Insert(ctx, domain.NewExecutionJob(task.ID, exec.ID))

	if _, err := jobRepo.Claim(ctx, exec.ID, "worker-1", time.Minute); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	// Never called MarkRunning — the job is CLAIMED, not RUNNING, simulating a worker that
	// crashed between Claim and MarkRunning (or was already reclaimed).
	if err := jobRepo.MarkDone(ctx, exec.ID); !errors.Is(err, application.ErrJobClaimLost) {
		t.Fatalf("expected ErrJobClaimLost marking done a job that is CLAIMED, not RUNNING, got %v", err)
	}
	if err := jobRepo.MarkFailed(ctx, exec.ID, "boom"); !errors.Is(err, application.ErrJobClaimLost) {
		t.Fatalf("expected ErrJobClaimLost marking failed a job that is CLAIMED, not RUNNING, got %v", err)
	}

	final, err := jobRepo.FindByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if final.Status != domain.JobClaimed {
		t.Fatalf("expected the job to still be CLAIMED (untouched by the rejected writes), got %s", final.Status)
	}
}
