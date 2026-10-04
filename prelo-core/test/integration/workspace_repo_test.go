package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
)

// PR-1: migration 00024 creates exactly one workspace with a stable id, and a second row is refused.
func TestWorkspaceIsASingleStableRow(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := postgres.NewWorkspaceRepository(pool)
	first, err := repo.Current(ctx)
	if err != nil || first.ID == uuid.Nil || first.Name != "Prelo Control" {
		t.Fatalf("workspace: %+v %v", first, err)
	}
	again, _ := repo.Current(ctx)
	if again.ID != first.ID {
		t.Fatal("workspace id must be stable")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspace DEFAULT VALUES`); err == nil {
		t.Fatal("a second workspace row must be refused (singleton)")
	}
}
