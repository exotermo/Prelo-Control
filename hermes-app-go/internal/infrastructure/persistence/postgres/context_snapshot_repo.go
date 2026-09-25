package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

type ContextSnapshotRepository struct {
	pool *pgxpool.Pool
}

func NewContextSnapshotRepository(pool *pgxpool.Pool) *ContextSnapshotRepository {
	return &ContextSnapshotRepository{pool: pool}
}

func (r *ContextSnapshotRepository) Save(ctx context.Context, snapshot domain.ContextSnapshot) (domain.ContextSnapshot, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ContextSnapshot{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO context_snapshots (id, task_id, version, resolved_at)
		VALUES ($1, $2, $3, $4)`,
		snapshot.ID.Value, snapshot.TaskID.Value, snapshot.Version, snapshot.ResolvedAt); err != nil {
		return domain.ContextSnapshot{}, err
	}

	for _, item := range snapshot.Items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO context_snapshot_items
				(id, snapshot_id, name, content, source_type, provenance, item_order)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)`,
			snapshot.ID.Value, item.Name, item.Content, string(item.SourceType), item.Provenance, item.Order); err != nil {
			return domain.ContextSnapshot{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ContextSnapshot{}, err
	}
	return snapshot, nil
}

func (r *ContextSnapshotRepository) FindByID(ctx context.Context, id domain.ContextSnapshotID) (domain.ContextSnapshot, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, version, resolved_at FROM context_snapshots WHERE id = $1`, id.Value)

	var s domain.ContextSnapshot
	if err := row.Scan(&s.ID.Value, &s.TaskID.Value, &s.Version, &s.ResolvedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ContextSnapshot{}, application.ErrContextSnapshotNotFound
		}
		return domain.ContextSnapshot{}, err
	}

	items, err := r.findItems(ctx, s.ID)
	if err != nil {
		return domain.ContextSnapshot{}, err
	}
	s.Items = items
	return s, nil
}

func (r *ContextSnapshotRepository) FindLatestByTaskID(ctx context.Context, taskID domain.TaskID) (domain.ContextSnapshot, bool, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, version, resolved_at FROM context_snapshots
		 WHERE task_id = $1 ORDER BY version DESC LIMIT 1`, taskID.Value)

	var s domain.ContextSnapshot
	if err := row.Scan(&s.ID.Value, &s.TaskID.Value, &s.Version, &s.ResolvedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ContextSnapshot{}, false, nil
		}
		return domain.ContextSnapshot{}, false, err
	}

	items, err := r.findItems(ctx, s.ID)
	if err != nil {
		return domain.ContextSnapshot{}, false, err
	}
	s.Items = items
	return s, true, nil
}

func (r *ContextSnapshotRepository) findItems(ctx context.Context, snapshotID domain.ContextSnapshotID) ([]domain.ContextSnapshotItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT name, content, source_type, provenance, item_order
		  FROM context_snapshot_items WHERE snapshot_id = $1 ORDER BY item_order ASC`, snapshotID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.ContextSnapshotItem
	for rows.Next() {
		var item domain.ContextSnapshotItem
		var sourceType string
		if err := rows.Scan(&item.Name, &item.Content, &sourceType, &item.Provenance, &item.Order); err != nil {
			return nil, err
		}
		item.SourceType = domain.ContextSourceType(sourceType)
		items = append(items, item)
	}
	return items, rows.Err()
}
