package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// Insert retries with a fresh short code on the (rare) clash with another pending request's code.
func (r *ApprovalRepository) Insert(ctx context.Context, approval domain.ApprovalRequest) error {
	for attempt := 0; ; attempt++ {
		var code *string
		if approval.ShortCode != "" {
			code = &approval.ShortCode
		}
		_, err := r.pool.Exec(ctx, `
			INSERT INTO approval_requests
				(id, tool_call_id, scope, status, requested_at, expires_at, decided_at, decided_by, approval_version, short_code, action_request_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			approval.ID.Value, toolCallIDValue(approval), approval.Scope, string(approval.Status),
			approval.RequestedAt, approval.ExpiresAt, approval.DecidedAt, approval.DecidedBy, approval.Version, code, approval.ActionRequestID)
		var pgErr *pgconn.PgError
		if err != nil && errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "approval_requests_pending_code_uk" && attempt < 8 {
			approval.ShortCode = domain.NewShortCode()
			continue
		}
		return err
	}
}

// FindLatestByShortCode returns the newest request with this code (pending or not — the caller
// tells the owner "already decided" / "expired" instead of "not found").
func (r *ApprovalRepository) FindLatestByShortCode(ctx context.Context, code string) (domain.ApprovalRequest, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+approvalColumns+` FROM approval_requests
		WHERE short_code = $1 ORDER BY (status = 'PENDING') DESC, requested_at DESC LIMIT 1`, code)
	return scanApproval(row)
}

func (r *ApprovalRepository) ListDuePending(ctx context.Context, limit int) ([]domain.ApprovalRequest, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+approvalColumns+` FROM approval_requests
		WHERE status = 'PENDING' AND expires_at < now() ORDER BY expires_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ApprovalRequest
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const approvalColumns = "id, tool_call_id, scope, status, requested_at, expires_at, decided_at, decided_by, approval_version, short_code, action_request_id"

// toolCallIDValue writes NULL for an action approval (PR-3), whose subject is not a tool call.
func toolCallIDValue(a domain.ApprovalRequest) *uuid.UUID {
	if a.IsAction() || a.ToolCallID.Value == uuid.Nil {
		return nil
	}
	v := a.ToolCallID.Value
	return &v
}

func (r *ApprovalRepository) FindByID(ctx context.Context, id domain.ApprovalRequestID) (domain.ApprovalRequest, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tool_call_id, scope, status, requested_at, expires_at, decided_at, decided_by, approval_version, short_code, action_request_id
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
		SELECT id, tool_call_id, scope, status, requested_at, expires_at, decided_at, decided_by, approval_version, short_code, action_request_id
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
	// Tool-call approvals reach the project through the task; action approvals (PR-3) through the
	// action request. Either way, the same "which project" rule.
	const base = `
		SELECT ar.id, ar.tool_call_id, ar.scope, ar.status, ar.requested_at, ar.expires_at, ar.decided_at, ar.decided_by, ar.approval_version, ar.short_code, ar.action_request_id
		  FROM approval_requests ar
		  LEFT JOIN tool_calls tc ON tc.id = ar.tool_call_id
		  LEFT JOIN tasks t ON t.id = tc.task_id
		  LEFT JOIN action_requests act ON act.id = ar.action_request_id
		 WHERE ar.status = 'PENDING'`
	var rows pgx.Rows
	var err error
	if projectID == nil {
		rows, err = r.pool.Query(ctx, base+" AND ar.tool_call_id IS NOT NULL AND t.project_id IS NULL ORDER BY ar.requested_at")
	} else {
		rows, err = r.pool.Query(ctx, base+" AND coalesce(t.project_id, act.project_id) = $1 ORDER BY ar.requested_at", *projectID)
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
	var code *string
	var toolCallID *uuid.UUID
	if err := row.Scan(&a.ID.Value, &toolCallID, &a.Scope, &status, &a.RequestedAt,
		&a.ExpiresAt, &a.DecidedAt, &a.DecidedBy, &a.Version, &code, &a.ActionRequestID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApprovalRequest{}, application.ErrApprovalNotFound
		}
		return domain.ApprovalRequest{}, err
	}
	a.Status = domain.ApprovalStatus(status)
	if code != nil {
		a.ShortCode = *code
	}
	if toolCallID != nil {
		a.ToolCallID.Value = *toolCallID
	}
	return a, nil
}
