package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

type ExecutionSuspensionRepository struct {
	pool *pgxpool.Pool
}

func NewExecutionSuspensionRepository(pool *pgxpool.Pool) *ExecutionSuspensionRepository {
	return &ExecutionSuspensionRepository{pool: pool}
}

func (r *ExecutionSuspensionRepository) Insert(ctx context.Context, suspension domain.ExecutionSuspension) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO execution_suspensions (id, execution_id, reason, resume_key, created_at, resolved_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		suspension.ID.Value, suspension.ExecutionID.Value, string(suspension.Reason),
		suspension.ResumeKey, suspension.CreatedAt, suspension.ResolvedAt)
	return err
}

func (r *ExecutionSuspensionRepository) FindActiveByResumeKey(ctx context.Context, reason domain.SuspensionReason, resumeKey string) (domain.ExecutionSuspension, bool, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, execution_id, reason, resume_key, created_at, resolved_at
		  FROM execution_suspensions WHERE reason = $1 AND resume_key = $2 AND resolved_at IS NULL`,
		string(reason), resumeKey)
	suspension, err := scanSuspension(row)
	if err != nil {
		if errors.Is(err, application.ErrExecutionSuspensionNotFound) {
			return domain.ExecutionSuspension{}, false, nil
		}
		return domain.ExecutionSuspension{}, false, err
	}
	return suspension, true, nil
}

// FindActiveByExecutionID is the Pipeline page's (Fase P) direct lookup — given an execution,
// is it suspended right now, and why? The complement of FindActiveByResumeKey, which goes the
// other direction (used when something arrives to resolve a suspension).
func (r *ExecutionSuspensionRepository) FindActiveByExecutionID(ctx context.Context, executionID domain.ExecutionID) (domain.ExecutionSuspension, bool, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, execution_id, reason, resume_key, created_at, resolved_at
		  FROM execution_suspensions WHERE execution_id = $1 AND resolved_at IS NULL`,
		executionID.Value)
	suspension, err := scanSuspension(row)
	if err != nil {
		if errors.Is(err, application.ErrExecutionSuspensionNotFound) {
			return domain.ExecutionSuspension{}, false, nil
		}
		return domain.ExecutionSuspension{}, false, err
	}
	return suspension, true, nil
}

func (r *ExecutionSuspensionRepository) Resolve(ctx context.Context, id domain.ExecutionSuspensionID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE execution_suspensions SET resolved_at = now() WHERE id = $1 AND resolved_at IS NULL`, id.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrExecutionSuspensionAlreadyResolved
	}
	return nil
}

func scanSuspension(row pgx.Row) (domain.ExecutionSuspension, error) {
	var s domain.ExecutionSuspension
	var reason string
	if err := row.Scan(&s.ID.Value, &s.ExecutionID.Value, &reason, &s.ResumeKey, &s.CreatedAt, &s.ResolvedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ExecutionSuspension{}, application.ErrExecutionSuspensionNotFound
		}
		return domain.ExecutionSuspension{}, err
	}
	s.Reason = domain.SuspensionReason(reason)
	return s, nil
}
