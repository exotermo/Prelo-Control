package integration

import (
	"context"
	"testing"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/persistence/postgres"
)

func TestManualContextRepository_SaveAndFindPreservesOrder(t *testing.T) {
	pool := newTestPool(t)
	taskRepo := postgres.NewTaskRepository(pool)
	manualRepo := postgres.NewManualContextRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(ctx, task)

	first, _ := domain.NewManualContextItem("first", "content 1")
	second, _ := domain.NewManualContextItem("second", "content 2")
	if err := manualRepo.Save(ctx, task.ID, []domain.ManualContextItem{first, second}); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	found, err := manualRepo.FindByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if len(found) != 2 || found[0].Name != "first" || found[1].Name != "second" {
		t.Fatalf("unexpected order: %+v", found)
	}
}

func TestContextSnapshotRepository_SaveFindLatestVersioning(t *testing.T) {
	pool := newTestPool(t)
	taskRepo := postgres.NewTaskRepository(pool)
	snapshotRepo := postgres.NewContextSnapshotRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(ctx, task)

	item, _ := domain.NewContextSnapshotItem("description", "desc", domain.ContextSourceManual, "task-description", 0)
	snapshot1, _ := domain.ResolveContextSnapshot(task.ID, 1, []domain.ContextSnapshotItem{item})
	if _, err := snapshotRepo.Save(ctx, snapshot1); err != nil {
		t.Fatalf("save v1 failed: %v", err)
	}

	latest, found, err := snapshotRepo.FindLatestByTaskID(ctx, task.ID)
	if err != nil || !found || latest.Version != 1 {
		t.Fatalf("expected latest version 1, got %+v found=%v err=%v", latest, found, err)
	}

	snapshot2, _ := domain.ResolveContextSnapshot(task.ID, 2, []domain.ContextSnapshotItem{item})
	if _, err := snapshotRepo.Save(ctx, snapshot2); err != nil {
		t.Fatalf("save v2 failed: %v", err)
	}

	latest, found, err = snapshotRepo.FindLatestByTaskID(ctx, task.ID)
	if err != nil || !found || latest.Version != 2 {
		t.Fatalf("expected latest version 2, got %+v found=%v err=%v", latest, found, err)
	}

	fetched, err := snapshotRepo.FindByID(ctx, latest.ID)
	if err != nil {
		t.Fatalf("find by id failed: %v", err)
	}
	if len(fetched.Items) != 1 || fetched.Items[0].Content != "desc" {
		t.Fatalf("unexpected fetched items: %+v", fetched.Items)
	}
}

func TestContextSnapshotRepository_FindLatest_NoneYet(t *testing.T) {
	pool := newTestPool(t)
	taskRepo := postgres.NewTaskRepository(pool)
	snapshotRepo := postgres.NewContextSnapshotRepository(pool)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(ctx, task)

	_, found, err := snapshotRepo.FindLatestByTaskID(ctx, task.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected no snapshot yet")
	}
}

// TestContextResolver_OrderAndVersioning mirrors ContextResolverTest: item 0 is always the
// task description, followed by manual items in stored order, and each resolve increments
// the snapshot version for the task.
func TestContextResolver_OrderAndVersioning(t *testing.T) {
	pool := newTestPool(t)
	taskRepo := postgres.NewTaskRepository(pool)
	manualRepo := postgres.NewManualContextRepository(pool)
	snapshotRepo := postgres.NewContextSnapshotRepository(pool)
	resolver := application.NewContextResolver(manualRepo, snapshotRepo)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("do the thing", agentID)
	_ = taskRepo.Insert(ctx, task)

	first, _ := domain.NewManualContextItem("first", "content 1")
	second, _ := domain.NewManualContextItem("second", "content 2")
	_ = manualRepo.Save(ctx, task.ID, []domain.ManualContextItem{first, second})

	snapshot, err := resolver.Resolve(ctx, task)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(snapshot.Items) != 3 {
		t.Fatalf("expected 3 items (description + 2 manual), got %d", len(snapshot.Items))
	}
	if snapshot.Items[0].Name != "description" || snapshot.Items[0].Content != "do the thing" || snapshot.Items[0].Order != 0 {
		t.Fatalf("unexpected item 0: %+v", snapshot.Items[0])
	}
	if snapshot.Items[1].Name != "first" || snapshot.Items[1].Order != 1 {
		t.Fatalf("unexpected item 1: %+v", snapshot.Items[1])
	}
	if snapshot.Items[2].Name != "second" || snapshot.Items[2].Order != 2 {
		t.Fatalf("unexpected item 2: %+v", snapshot.Items[2])
	}
	if snapshot.Version != 1 {
		t.Fatalf("expected first snapshot version 1, got %d", snapshot.Version)
	}

	if _, err := snapshotRepo.Save(ctx, snapshot); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	second2, err := resolver.Resolve(ctx, task)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if second2.Version != 2 {
		t.Fatalf("expected second snapshot version 2, got %d", second2.Version)
	}
}
