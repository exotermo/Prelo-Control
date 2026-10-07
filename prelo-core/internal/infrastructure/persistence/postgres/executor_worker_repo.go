package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
)

type ExecutorWorkerRepository struct{ pool *pgxpool.Pool }

func NewExecutorWorkerRepository(pool *pgxpool.Pool) *ExecutorWorkerRepository {
	return &ExecutorWorkerRepository{pool: pool}
}

func (r *ExecutorWorkerRepository) Insert(ctx context.Context, w application.ExecutorWorker) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO executor_workers
		(id,name,project_id,image_digest,token_hash,enabled,created_by,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, w.ID, w.Name, w.ProjectID.Value, w.ImageDigest, w.TokenHash, w.Enabled, w.CreatedBy, w.CreatedAt)
	return err
}

const executorWorkerSelect = `SELECT id,name,project_id,image_digest,token_hash,enabled,created_by,created_at,last_seen_at FROM executor_workers`

func (r *ExecutorWorkerRepository) FindByID(ctx context.Context, id uuid.UUID) (application.ExecutorWorker, error) {
	return scanExecutorWorker(r.pool.QueryRow(ctx, executorWorkerSelect+` WHERE id=$1`, id))
}

func scanExecutorWorker(row pgx.Row) (application.ExecutorWorker, error) {
	var w application.ExecutorWorker
	err := row.Scan(&w.ID, &w.Name, &w.ProjectID.Value, &w.ImageDigest, &w.TokenHash, &w.Enabled, &w.CreatedBy, &w.CreatedAt, &w.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ExecutorWorker{}, application.ErrExecutorWorkerNotFound
	}
	return w, err
}

func (r *ExecutorWorkerRepository) FindByTokenHash(ctx context.Context, hash []byte) (application.ExecutorWorker, error) {
	return scanExecutorWorker(r.pool.QueryRow(ctx, executorWorkerSelect+` WHERE token_hash=$1 AND enabled=TRUE
		AND EXISTS (SELECT 1 FROM projects p WHERE p.id=executor_workers.project_id AND p.deleted_at IS NULL)`, hash))
}

func (r *ExecutorWorkerRepository) List(ctx context.Context) ([]application.ExecutorWorker, error) {
	rows, err := r.pool.Query(ctx, executorWorkerSelect+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []application.ExecutorWorker{}
	for rows.Next() {
		w, err := scanExecutorWorker(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func (r *ExecutorWorkerRepository) Touch(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE executor_workers SET last_seen_at=now() WHERE id=$1 AND enabled=TRUE
		AND EXISTS (SELECT 1 FROM projects p WHERE p.id=executor_workers.project_id AND p.deleted_at IS NULL)`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrExecutorWorkerUnauthorized
	}
	return nil
}

func (r *ExecutorWorkerRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE executor_workers SET enabled=FALSE WHERE id=$1 AND enabled=TRUE`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrExecutorWorkerNotFound
	}
	return nil
}
