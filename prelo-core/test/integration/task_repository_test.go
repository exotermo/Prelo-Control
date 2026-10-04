package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
)

func TestTaskRepository_InsertFindUpdate(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewTaskRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("do the thing", agentID)

	if err := repo.Insert(ctx, task); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	found, err := repo.FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if found.Status != domain.TaskCreated || found.Description != "do the thing" {
		t.Fatalf("unexpected task: %+v", found)
	}

	running, err := found.Running()
	if err != nil {
		t.Fatalf("unexpected transition error: %v", err)
	}
	updated, err := repo.Update(ctx, running)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.Status != domain.TaskRunning || updated.Version != found.Version+1 {
		t.Fatalf("unexpected updated task: %+v", updated)
	}
}

func TestTaskRepository_FindByID_NotFound(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewTaskRepository(pool)

	_, err := repo.FindByID(context.Background(), domain.NewTaskID())
	if !errors.Is(err, application.ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestTaskRepository_Update_OptimisticLockOnStaleVersion(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewTaskRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("do the thing", agentID)
	if err := repo.Insert(ctx, task); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	running, _ := task.Running()
	if _, err := repo.Update(ctx, running); err != nil {
		t.Fatalf("first update failed: %v", err)
	}

	// task still carries the stale (pre-update) version — a second write against it must lose.
	staleRunning, _ := task.Running()
	if _, err := repo.Update(ctx, staleRunning); !errors.Is(err, application.ErrOptimisticLock) {
		t.Fatalf("expected ErrOptimisticLock, got %v", err)
	}
}

// TestTaskRepository_ConcurrentExecuteClaim mirrors ExecuteTaskUseCaseConcurrencyIT: N
// goroutines race to move the same task CREATED->RUNNING; exactly one must win.
func TestTaskRepository_ConcurrentExecuteClaim(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewTaskRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("do the thing", agentID)
	if err := repo.Insert(ctx, task); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	const attempts = 8
	var wins int64
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			defer wg.Done()
			found, err := repo.FindByID(ctx, task.ID)
			if err != nil {
				t.Errorf("find failed: %v", err)
				return
			}
			running, err := found.Running()
			if err != nil {
				// Another goroutine may have already advanced status past CREATED/QUEUED
				// by the time this read happened; that's a legitimate loss too.
				return
			}
			if _, err := repo.Update(ctx, running); err == nil {
				atomic.AddInt64(&wins, 1)
			} else if !errors.Is(err, application.ErrOptimisticLock) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("expected exactly 1 winning claim, got %d", wins)
	}

	final, err := repo.FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if final.Status != domain.TaskRunning {
		t.Fatalf("expected final status RUNNING, got %s", final.Status)
	}
}
