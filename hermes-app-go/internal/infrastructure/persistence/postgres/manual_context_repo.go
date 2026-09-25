package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

type ManualContextRepository struct {
	pool *pgxpool.Pool
}

func NewManualContextRepository(pool *pgxpool.Pool) *ManualContextRepository {
	return &ManualContextRepository{pool: pool}
}

func (r *ManualContextRepository) Save(ctx context.Context, taskID domain.TaskID, items []domain.ManualContextItem) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for order, item := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_manual_context_items (id, task_id, name, content, item_order)
			VALUES (gen_random_uuid(), $1, $2, $3, $4)`,
			taskID.Value, item.Name, item.Content, order); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *ManualContextRepository) FindByTaskID(ctx context.Context, taskID domain.TaskID) ([]domain.ManualContextItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT name, content FROM task_manual_context_items
		 WHERE task_id = $1 ORDER BY item_order ASC`, taskID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.ManualContextItem
	for rows.Next() {
		var name, content string
		if err := rows.Scan(&name, &content); err != nil {
			return nil, err
		}
		items = append(items, domain.ManualContextItem{Name: name, Content: content})
	}
	return items, rows.Err()
}
