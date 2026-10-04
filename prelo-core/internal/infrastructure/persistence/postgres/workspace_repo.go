package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/domain"
)

// WorkspaceRepository reads the instance's single workspace row (migration 00024).
type WorkspaceRepository struct{ pool *pgxpool.Pool }

func NewWorkspaceRepository(pool *pgxpool.Pool) *WorkspaceRepository {
	return &WorkspaceRepository{pool: pool}
}

func (r *WorkspaceRepository) Current(ctx context.Context) (domain.Workspace, error) {
	var w domain.Workspace
	err := r.pool.QueryRow(ctx, `SELECT id, name, created_at FROM workspace WHERE singleton`).Scan(&w.ID, &w.Name, &w.CreatedAt)
	return w, err
}
