package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/persistence/postgres"
)

func TestExecutionRepository_InsertFindUpdate(t *testing.T) {
	pool := newTestPool(t)
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("do the thing", agentID)
	if err := taskRepo.Insert(ctx, task); err != nil {
		t.Fatalf("insert task failed: %v", err)
	}

	exec := domain.NewPendingExecution(task.ID, agentID)
	if err := execRepo.Insert(ctx, exec); err != nil {
		t.Fatalf("insert execution failed: %v", err)
	}

	found, err := execRepo.FindByID(ctx, exec.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if found.Status != domain.ExecutionPending {
		t.Fatalf("expected PENDING, got %s", found.Status)
	}

	// ContextSnapshot persistence/linking is exercised once the context repository lands
	// (etapa G5); here we only cover Execution's own status/result lifecycle.
	running := found.Running()
	updated, err := execRepo.Update(ctx, running)
	if err != nil {
		t.Fatalf("update to running failed: %v", err)
	}
	if updated.Status != domain.ExecutionRunning {
		t.Fatalf("unexpected updated execution: %+v", updated)
	}

	completed := updated.Completed("the answer", "req-1", "claude-x", "anthropic")
	final, err := execRepo.Update(ctx, completed)
	if err != nil {
		t.Fatalf("update to completed failed: %v", err)
	}
	if final.Status != domain.ExecutionCompleted || final.Result == nil || *final.Result != "the answer" {
		t.Fatalf("unexpected final execution: %+v", final)
	}
}

func TestExecutionRepository_Update_OptimisticLock(t *testing.T) {
	pool := newTestPool(t)
	taskRepo := postgres.NewTaskRepository(pool)
	execRepo := postgres.NewExecutionRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("do the thing", agentID)
	_ = taskRepo.Insert(ctx, task)

	exec := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(ctx, exec)

	running := exec.Running()
	if _, err := execRepo.Update(ctx, running); err != nil {
		t.Fatalf("first update failed: %v", err)
	}

	staleRunning := exec.Running() // still carries version 0
	if _, err := execRepo.Update(ctx, staleRunning); !errors.Is(err, application.ErrOptimisticLock) {
		t.Fatalf("expected ErrOptimisticLock, got %v", err)
	}
}

func TestExecutionRepository_FindByID_NotFound(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewExecutionRepository(pool)

	_, err := repo.FindByID(context.Background(), domain.NewExecutionID())
	if !errors.Is(err, application.ErrExecutionNotFound) {
		t.Fatalf("expected ErrExecutionNotFound, got %v", err)
	}
}
