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

type ApprovalRepository struct {
	pool *pgxpool.Pool
}

func NewApprovalRepository(pool *pgxpool.Pool) *ApprovalRepository {
	return &ApprovalRepository{pool: pool}
}

func (r *ApprovalRepository) Insert(ctx context.Context, approval domain.ApprovalRequest) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO approval_requests
			(id, tool_call_id, scope, status, requested_at, expires_at, decided_at, decided_by, approval_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		approval.ID.Value, approval.ToolCallID.Value, approval.Scope, string(approval.Status),
		approval.RequestedAt, approval.ExpiresAt, approval.DecidedAt, approval.DecidedBy, approval.Version)
	return err
}

func (r *ApprovalRepository) FindByID(ctx context.Context, id domain.ApprovalRequestID) (domain.ApprovalRequest, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tool_call_id, scope, status, requested_at, expires_at, decided_at, decided_by, approval_version
		  FROM approval_requests WHERE id = $1`, id.Value)
	return scanApproval(row)
}

// Update performs a version-checked write, same discipline as every other repository's Update.
func (r *ApprovalRepository) Update(ctx context.Context, approval domain.ApprovalRequest) (domain.ApprovalRequest, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE approval_requests
		   SET status = $2, decided_at = $3, decided_by = $4, approval_version = approval_version + 1
		 WHERE id = $1 AND approval_version = $5
		 RETURNING approval_version`,
		approval.ID.Value, string(approval.Status), approval.DecidedAt, approval.DecidedBy, approval.Version)

	var newVersion int64
	if err := row.Scan(&newVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApprovalRequest{}, application.ErrOptimisticLock
		}
		return domain.ApprovalRequest{}, err
	}
	return approval.WithVersion(newVersion), nil
}

func (r *ApprovalRepository) ListPending(ctx context.Context) ([]domain.ApprovalRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tool_call_id, scope, status, requested_at, expires_at, decided_at, decided_by, approval_version
		  FROM approval_requests WHERE status = 'PENDING' ORDER BY requested_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []domain.ApprovalRequest
	for rows.Next() {
		approval, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, approval)
	}
	return results, rows.Err()
}

// ListPendingByProject backs the Fase W project-scoped Aprovações page. Unlike every other
// ...ByProject method in this package, it needs a JOIN — approval_requests carries no
// project_id of its own, only a tool_call_id, and a ToolCall's Task is what's actually scoped.
// nil projectID means the "unassigned" bucket (tasks.project_id IS NULL).
func (r *ApprovalRepository) ListPendingByProject(ctx context.Context, projectID *uuid.UUID) ([]domain.ApprovalRequest, error) {
	const base = `
		SELECT ar.id, ar.tool_call_id, ar.scope, ar.status, ar.requested_at, ar.expires_at, ar.decided_at, ar.decided_by, ar.approval_version
		  FROM approval_requests ar
		  JOIN tool_calls tc ON tc.id = ar.tool_call_id
		  JOIN tasks t ON t.id = tc.task_id
		 WHERE ar.status = 'PENDING'`
	var rows pgx.Rows
	var err error
	if projectID == nil {
		rows, err = r.pool.Query(ctx, base+" AND t.project_id IS NULL ORDER BY ar.requested_at")
	} else {
		rows, err = r.pool.Query(ctx, base+" AND t.project_id = $1 ORDER BY ar.requested_at", *projectID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []domain.ApprovalRequest
	for rows.Next() {
		approval, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, approval)
	}
	return results, rows.Err()
}

func scanApproval(row pgx.Row) (domain.ApprovalRequest, error) {
	var a domain.ApprovalRequest
	var status string
	if err := row.Scan(&a.ID.Value, &a.ToolCallID.Value, &a.Scope, &status, &a.RequestedAt,
		&a.ExpiresAt, &a.DecidedAt, &a.DecidedBy, &a.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApprovalRequest{}, application.ErrApprovalNotFound
		}
		return domain.ApprovalRequest{}, err
	}
	a.Status = domain.ApprovalStatus(status)
	return a, nil
}
