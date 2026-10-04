package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// ActionRequestRepository (PR-3): external action requests, their approval and reported results.
type ActionRequestRepository struct{ pool *pgxpool.Pool }

func NewActionRequestRepository(pool *pgxpool.Pool) *ActionRequestRepository {
	return &ActionRequestRepository{pool: pool}
}

const actionViewSelect = `SELECT a.id, a.workspace_id, a.project_id, a.kind, a.payload, a.payload_hash, a.risk, a.impact,
	a.requested_by, a.idempotency_key, a.api_key_id, a.created_at, a.result_status, a.result_sequence, a.result_message,
	a.result_url, a.result_digest, a.result_reported_at,
	ar.id, ar.scope, ar.status, ar.requested_at, ar.expires_at, ar.decided_at, ar.decided_by, ar.approval_version, ar.short_code
	  FROM action_requests a JOIN approval_requests ar ON ar.action_request_id = a.id`

func scanActionView(row pgx.Row) (application.ActionView, error) {
	var v application.ActionView
	a := &v.Action
	var risk, approvalStatus string
	var resultStatus, resultMessage, resultURL, resultDigest, code *string
	var resultSequence *int
	var resultAt *time.Time
	err := row.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID.Value, &a.Kind, &a.Payload, &a.PayloadHash, &risk, &a.Impact,
		&a.RequestedBy, &a.IdempotencyKey, &a.ApiKeyID, &a.CreatedAt, &resultStatus, &resultSequence, &resultMessage,
		&resultURL, &resultDigest, &resultAt,
		&v.Approval.ID.Value, &v.Approval.Scope, &approvalStatus, &v.Approval.RequestedAt, &v.Approval.ExpiresAt,
		&v.Approval.DecidedAt, &v.Approval.DecidedBy, &v.Approval.Version, &code)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ActionView{}, application.ErrActionRequestNotFound
	}
	if err != nil {
		return application.ActionView{}, err
	}
	a.Risk = domain.RiskLevel(risk)
	id := a.ID
	v.Approval.ActionRequestID = &id
	v.Approval.Status = domain.ApprovalStatus(approvalStatus)
	if code != nil {
		v.Approval.ShortCode = *code
	}
	if resultStatus != nil && resultSequence != nil && resultAt != nil {
		a.Result = &domain.ActionResult{Status: *resultStatus, Sequence: *resultSequence, Message: resultMessage,
			URL: resultURL, Digest: resultDigest, ReportedAt: *resultAt}
	}
	return v, nil
}

func (r *ActionRequestRepository) InsertWithApproval(ctx context.Context, a domain.ActionRequest, approval domain.ApprovalRequest) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO action_requests
		(id, workspace_id, project_id, kind, payload, payload_hash, risk, impact, requested_by, idempotency_key, api_key_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		a.ID, a.WorkspaceID, a.ProjectID.Value, a.Kind, a.Payload, a.PayloadHash, string(a.Risk), a.Impact, a.RequestedBy,
		a.IdempotencyKey, a.ApiKeyID, a.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return application.ErrIdempotencyConflict // a concurrent request with the same key won
		}
		return err
	}
	for attempt := 0; ; attempt++ {
		if _, err := tx.Exec(ctx, `SAVEPOINT approval_insert`); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO approval_requests
			(id, tool_call_id, scope, status, requested_at, expires_at, decided_at, decided_by, approval_version, short_code, action_request_id)
			VALUES ($1, NULL, $2, $3, $4, $5, NULL, NULL, 0, $6, $7)`,
			approval.ID.Value, approval.Scope, string(approval.Status), approval.RequestedAt, approval.ExpiresAt, approval.ShortCode, a.ID)
		var pgErr *pgconn.PgError
		if err != nil && errors.As(err, &pgErr) && pgErr.ConstraintName == "approval_requests_pending_code_uk" && attempt < 8 {
			if _, rbErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT approval_insert`); rbErr != nil {
				return rbErr
			}
			approval.ShortCode = domain.NewShortCode()
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	return tx.Commit(ctx)
}

func (r *ActionRequestRepository) FindByID(ctx context.Context, id uuid.UUID) (application.ActionView, error) {
	return scanActionView(r.pool.QueryRow(ctx, actionViewSelect+` WHERE a.id = $1`, id))
}

func (r *ActionRequestRepository) FindByIdempotencyKey(ctx context.Context, projectID domain.ProjectID, key string) (application.ActionView, bool, error) {
	v, err := scanActionView(r.pool.QueryRow(ctx, actionViewSelect+` WHERE a.project_id = $1 AND a.idempotency_key = $2`, projectID.Value, key))
	if errors.Is(err, application.ErrActionRequestNotFound) {
		return application.ActionView{}, false, nil
	}
	return v, err == nil, err
}

func (r *ActionRequestRepository) ListByProject(ctx context.Context, projectID domain.ProjectID, kind string, limit int) ([]application.ActionView, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, actionViewSelect+` WHERE a.project_id = $1 AND ($2 = '' OR a.kind = $2)
		ORDER BY a.created_at DESC LIMIT $3`, projectID.Value, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []application.ActionView{}
	for rows.Next() {
		v, err := scanActionView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *ActionRequestRepository) RecordResult(ctx context.Context, id uuid.UUID, res domain.ActionResult) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO action_request_results (action_request_id, sequence, status, message, url, digest, reported_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (action_request_id, sequence) DO NOTHING`,
		id, res.Sequence, res.Status, res.Message, res.URL, res.Digest, res.ReportedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE action_requests
		SET result_status = $2, result_sequence = $3, result_message = $4, result_url = $5, result_digest = $6, result_reported_at = $7
		WHERE id = $1 AND (result_sequence IS NULL OR result_sequence < $3)`,
		id, res.Status, res.Sequence, res.Message, res.URL, res.Digest, res.ReportedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
