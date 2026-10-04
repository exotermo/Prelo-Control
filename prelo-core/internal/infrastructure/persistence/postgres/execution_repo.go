package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type ExecutionRepository struct {
	pool *pgxpool.Pool
}

func NewExecutionRepository(pool *pgxpool.Pool) *ExecutionRepository {
	return &ExecutionRepository{pool: pool}
}

func (r *ExecutionRepository) Insert(ctx context.Context, e domain.Execution) error {
	var contextSnapshotID *uuid.UUID
	if e.ContextSnapshotID != nil {
		contextSnapshotID = &e.ContextSnapshotID.Value
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO task_executions
			(id, task_id, agent_id, status, started_at, completed_at, error, result,
			 request_id, model, provider, context_snapshot_id, agent_version, execution_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		e.ID.Value, e.TaskID.Value, e.AgentID.Value, string(e.Status), e.StartedAt, e.CompletedAt,
		e.Error, e.Result, e.RequestID, e.Model, e.Provider, contextSnapshotID, e.AgentVersion, e.Version)
	return err
}

func (r *ExecutionRepository) FindByID(ctx context.Context, id domain.ExecutionID) (domain.Execution, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, agent_id, status, started_at, completed_at, error, result,
		       request_id, model, provider, context_snapshot_id, agent_version, execution_version
		  FROM task_executions WHERE id = $1`, id.Value)
	return scanExecution(row)
}

// FindByTaskID assumes the current 1:1 Task→Execution relationship (EnqueueExecutionUseCase
// rejects a second enqueue of the same Task via Task.Queued()'s CREATED-only guard) — used by
// delegate_to_agent's caller (Fase C tests) and the observability API (Fase D) to go from a
// child TaskID to the Execution that actually ran it.
func (r *ExecutionRepository) FindByTaskID(ctx context.Context, taskID domain.TaskID) (domain.Execution, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, agent_id, status, started_at, completed_at, error, result,
		       request_id, model, provider, context_snapshot_id, agent_version, execution_version
		  FROM task_executions WHERE task_id = $1`, taskID.Value)
	return scanExecution(row)
}

// Update performs a version-checked write (`WHERE id=$1 AND execution_version=$2`).
// Execution transitions are unconditional in the domain (no status guard) — concurrency
// safety comes from the Task/job claim, not from this write — matching the Java entity's
// bare @Version usage.
func (r *ExecutionRepository) Update(ctx context.Context, e domain.Execution) (domain.Execution, error) {
	var contextSnapshotID *uuid.UUID
	if e.ContextSnapshotID != nil {
		contextSnapshotID = &e.ContextSnapshotID.Value
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE task_executions
		   SET status = $2, started_at = $3, completed_at = $4, error = $5, result = $6,
		       request_id = $7, model = $8, provider = $9, context_snapshot_id = $10,
		       agent_version = $11, execution_version = execution_version + 1
		 WHERE id = $1 AND execution_version = $12
		 RETURNING execution_version`,
		e.ID.Value, string(e.Status), e.StartedAt, e.CompletedAt, e.Error, e.Result,
		e.RequestID, e.Model, e.Provider, contextSnapshotID, e.AgentVersion, e.Version)

	var newVersion int64
	if err := row.Scan(&newVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Execution{}, application.ErrOptimisticLock
		}
		return domain.Execution{}, err
	}
	return e.WithVersion(newVersion), nil
}

func scanExecution(row pgx.Row) (domain.Execution, error) {
	var e domain.Execution
	var status string
	var agentID string
	var contextSnapshotID *uuid.UUID

	err := row.Scan(&e.ID.Value, &e.TaskID.Value, &agentID, &status, &e.StartedAt, &e.CompletedAt,
		&e.Error, &e.Result, &e.RequestID, &e.Model, &e.Provider, &contextSnapshotID, &e.AgentVersion, &e.Version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Execution{}, application.ErrExecutionNotFound
		}
		return domain.Execution{}, err
	}
	e.AgentID = domain.AgentID{Value: agentID}
	e.Status = domain.ExecutionStatus(status)
	if contextSnapshotID != nil {
		csID := domain.ContextSnapshotID{Value: *contextSnapshotID}
		e.ContextSnapshotID = &csID
	}
	return e, nil
}
