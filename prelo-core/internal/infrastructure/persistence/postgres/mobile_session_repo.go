package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// MobileSessionRepository (PR-2): app sessions + rotating refresh token hashes.
type MobileSessionRepository struct{ pool *pgxpool.Pool }

func NewMobileSessionRepository(pool *pgxpool.Pool) *MobileSessionRepository {
	return &MobileSessionRepository{pool: pool}
}

const mobileSessionColumns = `id, user_id, device_id, device_name, platform, created_at, last_used_at,
	idle_expires_at, absolute_expires_at, last_totp_at, revoked_at, revoked_reason`

func scanMobileSession(row pgx.Row) (domain.MobileSession, error) {
	var s domain.MobileSession
	err := row.Scan(&s.ID, &s.UserID.Value, &s.DeviceID, &s.DeviceName, &s.Platform, &s.CreatedAt, &s.LastUsedAt,
		&s.IdleExpiresAt, &s.AbsoluteExpiresAt, &s.LastTotpAt, &s.RevokedAt, &s.RevokedReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MobileSession{}, application.ErrMobileSessionNotFound
	}
	return s, err
}

func (r *MobileSessionRepository) Insert(ctx context.Context, s domain.MobileSession, tokenHash []byte) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO mobile_sessions (`+mobileSessionColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		s.ID, s.UserID.Value, s.DeviceID, s.DeviceName, s.Platform, s.CreatedAt, s.LastUsedAt,
		s.IdleExpiresAt, s.AbsoluteExpiresAt, s.LastTotpAt, s.RevokedAt, s.RevokedReason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO mobile_session_tokens (token_hash, session_id) VALUES ($1, $2)`, tokenHash, s.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *MobileSessionRepository) Rotate(ctx context.Context, oldHash, newHash []byte, deviceID uuid.UUID, now time.Time) (domain.MobileSession, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.MobileSession{}, err
	}
	defer tx.Rollback(ctx)
	var sessionID uuid.UUID
	var usedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT session_id, used_at FROM mobile_session_tokens WHERE token_hash = $1 FOR UPDATE`, oldHash).Scan(&sessionID, &usedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MobileSession{}, application.ErrMobileRefreshInvalid
	}
	if err != nil {
		return domain.MobileSession{}, err
	}
	session, err := scanMobileSession(tx.QueryRow(ctx, `SELECT `+mobileSessionColumns+` FROM mobile_sessions WHERE id = $1 FOR UPDATE`, sessionID))
	if err != nil {
		return domain.MobileSession{}, err
	}
	if usedAt != nil {
		// Reuse of a spent refresh token: someone else has a copy. Kill the whole session.
		if _, err := tx.Exec(ctx, `UPDATE mobile_sessions SET revoked_at = $2, revoked_reason = 'refresh_reused'
			WHERE id = $1 AND revoked_at IS NULL`, sessionID, now); err != nil {
			return domain.MobileSession{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.MobileSession{}, err
		}
		return domain.MobileSession{}, application.ErrMobileRefreshReused
	}
	if !session.Active(now) || session.DeviceID != deviceID {
		return domain.MobileSession{}, application.ErrMobileRefreshInvalid
	}
	idle := session.NextIdleExpiry(now)
	if _, err := tx.Exec(ctx, `UPDATE mobile_session_tokens SET used_at = $2 WHERE token_hash = $1`, oldHash, now); err != nil {
		return domain.MobileSession{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO mobile_session_tokens (token_hash, session_id, issued_at) VALUES ($1, $2, $3)`, newHash, sessionID, now); err != nil {
		return domain.MobileSession{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE mobile_sessions SET last_used_at = $2, idle_expires_at = $3 WHERE id = $1`, sessionID, now, idle); err != nil {
		return domain.MobileSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.MobileSession{}, err
	}
	session.LastUsedAt, session.IdleExpiresAt = now, idle
	return session, nil
}

func (r *MobileSessionRepository) FindByID(ctx context.Context, id uuid.UUID) (domain.MobileSession, error) {
	return scanMobileSession(r.pool.QueryRow(ctx, `SELECT `+mobileSessionColumns+` FROM mobile_sessions WHERE id = $1`, id))
}

func (r *MobileSessionRepository) FindByTokenHash(ctx context.Context, hash []byte) (domain.MobileSession, error) {
	return scanMobileSession(r.pool.QueryRow(ctx, `SELECT `+mobileSessionColumnsQualified+`
		FROM mobile_sessions s JOIN mobile_session_tokens t ON t.session_id = s.id WHERE t.token_hash = $1`, hash))
}

const mobileSessionColumnsQualified = `s.id, s.user_id, s.device_id, s.device_name, s.platform, s.created_at, s.last_used_at,
	s.idle_expires_at, s.absolute_expires_at, s.last_totp_at, s.revoked_at, s.revoked_reason`

func (r *MobileSessionRepository) ListActiveByUser(ctx context.Context, userID domain.DashboardUserID, now time.Time) ([]domain.MobileSession, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+mobileSessionColumns+` FROM mobile_sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND idle_expires_at > $2 AND absolute_expires_at > $2
		ORDER BY last_used_at DESC`, userID.Value, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MobileSession{}
	for rows.Next() {
		s, err := scanMobileSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *MobileSessionRepository) Revoke(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.pool.Exec(ctx, `UPDATE mobile_sessions SET revoked_at = now(), revoked_reason = $2 WHERE id = $1 AND revoked_at IS NULL`, id, reason)
	return err
}

func (r *MobileSessionRepository) RevokeAllForUser(ctx context.Context, userID domain.DashboardUserID, reason string) (int, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE mobile_sessions SET revoked_at = now(), revoked_reason = $2 WHERE user_id = $1 AND revoked_at IS NULL`, userID.Value, reason)
	return int(tag.RowsAffected()), err
}

func (r *MobileSessionRepository) TouchTotp(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE mobile_sessions SET last_totp_at = $2 WHERE id = $1`, id, at)
	return err
}
