package postgres

import (
	"context"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TaskEstimateRepository struct{ pool *pgxpool.Pool }

func NewTaskEstimateRepository(pool *pgxpool.Pool) *TaskEstimateRepository {
	return &TaskEstimateRepository{pool: pool}
}

func (r *TaskEstimateRepository) Samples(ctx context.Context, profile, kind, bucket string, limit int) ([]application.TaskEstimateSample, error) {
	rows, err := r.pool.Query(ctx, `SELECT input_tokens,output_tokens,duration_ms FROM prelo_app.execution_turns
		WHERE kind='LLM_CALL' AND model_profile=$1 AND task_kind=$2 AND
		CASE WHEN estimated_context_tokens<2048 THEN 'XS' WHEN estimated_context_tokens<8192 THEN 'S' WHEN estimated_context_tokens<24576 THEN 'M' ELSE 'L' END=$3
		AND input_tokens>0 AND output_tokens>0
		ORDER BY started_at DESC LIMIT $4`, profile, kind, bucket, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	samples := make([]application.TaskEstimateSample, 0)
	for rows.Next() {
		var sample application.TaskEstimateSample
		if err := rows.Scan(&sample.InputTokens, &sample.OutputTokens, &sample.DurationMs); err != nil {
			return nil, err
		}
		samples = append(samples, sample)
	}
	return samples, rows.Err()
}

func (r *TaskEstimateRepository) Capacity(ctx context.Context, projectID uuid.UUID) (*application.TaskEstimateCapacity, error) {
	var capacity application.TaskEstimateCapacity
	err := r.pool.QueryRow(ctx, `SELECT c.profile_id,c.available_slots,c.maximum_slots,c.active_containers,c.observed_at
		FROM prelo_app.executor_worker_capacity c JOIN prelo_app.executor_workers w ON w.id=c.worker_id
		JOIN prelo_app.projects p ON p.id=w.project_id WHERE w.project_id=$1 AND w.enabled AND p.deleted_at IS NULL
		AND c.observed_at>now()-interval '60 seconds' ORDER BY c.observed_at DESC LIMIT 1`, projectID).
		Scan(&capacity.ProfileID, &capacity.AvailableSlots, &capacity.MaximumSlots, &capacity.Active, &capacity.ObservedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &capacity, nil
}
