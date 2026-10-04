package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type DashboardUserRepository struct{ pool *pgxpool.Pool }

func NewDashboardUserRepository(pool *pgxpool.Pool) *DashboardUserRepository {
	return &DashboardUserRepository{pool: pool}
}

func (r *DashboardUserRepository) Insert(ctx context.Context, user domain.DashboardUser) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO dashboard_users (id, email, password_hash, totp_secret_encrypted, totp_enabled, failed_logins, locked_until, activated_at, created_at, role)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		user.ID.Value, user.Email, nullString(user.PasswordHash), user.TOTPSecretEncrypted, user.TOTPEnabled,
		user.FailedLogins, user.LockedUntil, user.ActivatedAt, user.CreatedAt, string(user.Role))
	return err
}

// List backs the Usuários page — every dashboard account, newest first.
func (r *DashboardUserRepository) List(ctx context.Context) ([]domain.DashboardUser, error) {
	rows, err := r.pool.Query(ctx, dashboardUserSelect+" ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []domain.DashboardUser
	for rows.Next() {
		user, err := scanDashboardUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (r *DashboardUserRepository) FindByID(ctx context.Context, id domain.DashboardUserID) (domain.DashboardUser, error) {
	return scanDashboardUser(r.pool.QueryRow(ctx, dashboardUserSelect+" WHERE id = $1", id.Value))
}

func (r *DashboardUserRepository) FindByEmail(ctx context.Context, email string) (domain.DashboardUser, bool, error) {
	user, err := scanDashboardUser(r.pool.QueryRow(ctx, dashboardUserSelect+" WHERE email = $1", email))
	if err != nil {
		if errors.Is(err, application.ErrDashboardUserNotFound) {
			return domain.DashboardUser{}, false, nil
		}
		return domain.DashboardUser{}, false, err
	}
	return user, true, nil
}

func (r *DashboardUserRepository) Update(ctx context.Context, user domain.DashboardUser) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE dashboard_users SET password_hash=$2, totp_secret_encrypted=$3, totp_enabled=$4,
		       failed_logins=$5, locked_until=$6, activated_at=$7, role=$8
		 WHERE id=$1`,
		user.ID.Value, nullString(user.PasswordHash), user.TOTPSecretEncrypted, user.TOTPEnabled,
		user.FailedLogins, user.LockedUntil, user.ActivatedAt, string(user.Role))
	return err
}

const dashboardUserSelect = `SELECT id, email, password_hash, totp_secret_encrypted, totp_enabled, failed_logins, locked_until, activated_at, created_at, role FROM dashboard_users`

func scanDashboardUser(row pgx.Row) (domain.DashboardUser, error) {
	var u domain.DashboardUser
	var passwordHash *string
	var role string
	if err := row.Scan(&u.ID.Value, &u.Email, &passwordHash, &u.TOTPSecretEncrypted, &u.TOTPEnabled,
		&u.FailedLogins, &u.LockedUntil, &u.ActivatedAt, &u.CreatedAt, &role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.DashboardUser{}, application.ErrDashboardUserNotFound
		}
		return domain.DashboardUser{}, err
	}
	if passwordHash != nil {
		u.PasswordHash = *passwordHash
	}
	u.Role = domain.DashboardRole(role)
	return u, nil
}

// --- auth tokens ---

type DashboardAuthTokenRepository struct{ pool *pgxpool.Pool }

func NewDashboardAuthTokenRepository(pool *pgxpool.Pool) *DashboardAuthTokenRepository {
	return &DashboardAuthTokenRepository{pool: pool}
}

func (r *DashboardAuthTokenRepository) Insert(ctx context.Context, token domain.DashboardAuthToken) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO dashboard_auth_tokens (id, user_id, token_hash, purpose, expires_at, consumed_at, attempts, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		token.ID.Value, token.UserID.Value, token.TokenHash, string(token.Purpose), token.ExpiresAt, token.ConsumedAt, token.Attempts, token.CreatedAt)
	return err
}

func (r *DashboardAuthTokenRepository) FindActiveByHash(ctx context.Context, purpose domain.DashboardTokenPurpose, tokenHash []byte) (domain.DashboardAuthToken, bool, error) {
	row := r.pool.QueryRow(ctx, dashboardTokenSelect+` WHERE purpose=$1 AND token_hash=$2 AND consumed_at IS NULL AND expires_at > now()`,
		string(purpose), tokenHash)
	token, err := scanDashboardToken(row)
	if err != nil {
		if errors.Is(err, application.ErrDashboardTokenNotFound) {
			return domain.DashboardAuthToken{}, false, nil
		}
		return domain.DashboardAuthToken{}, false, err
	}
	return token, true, nil
}

func (r *DashboardAuthTokenRepository) FindByID(ctx context.Context, id domain.DashboardAuthTokenID) (domain.DashboardAuthToken, error) {
	return scanDashboardToken(r.pool.QueryRow(ctx, dashboardTokenSelect+" WHERE id=$1", id.Value))
}

// Consume is a guarded UPDATE (WHERE consumed_at IS NULL): under a race between two requests
// presenting the same raw token, exactly one succeeds — the loser's caller must treat the token
// as not-found, same belt-and-suspenders pattern as the rest of this codebase's claim/consume
// operations (ADR-013).
func (r *DashboardAuthTokenRepository) Consume(ctx context.Context, id domain.DashboardAuthTokenID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE dashboard_auth_tokens SET consumed_at = now() WHERE id=$1 AND consumed_at IS NULL`, id.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrDashboardTokenNotFound
	}
	return nil
}

func (r *DashboardAuthTokenRepository) IncrementAttempts(ctx context.Context, id domain.DashboardAuthTokenID) (int, error) {
	var attempts int
	err := r.pool.QueryRow(ctx, `UPDATE dashboard_auth_tokens SET attempts = attempts + 1 WHERE id=$1 RETURNING attempts`, id.Value).Scan(&attempts)
	return attempts, err
}

const dashboardTokenSelect = `SELECT id, user_id, token_hash, purpose, expires_at, consumed_at, attempts, created_at FROM dashboard_auth_tokens`

func scanDashboardToken(row pgx.Row) (domain.DashboardAuthToken, error) {
	var t domain.DashboardAuthToken
	var purpose string
	if err := row.Scan(&t.ID.Value, &t.UserID.Value, &t.TokenHash, &purpose, &t.ExpiresAt, &t.ConsumedAt, &t.Attempts, &t.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.DashboardAuthToken{}, application.ErrDashboardTokenNotFound
		}
		return domain.DashboardAuthToken{}, err
	}
	t.Purpose = domain.DashboardTokenPurpose(purpose)
	return t, nil
}

// --- recovery codes ---

type DashboardRecoveryCodeRepository struct{ pool *pgxpool.Pool }

func NewDashboardRecoveryCodeRepository(pool *pgxpool.Pool) *DashboardRecoveryCodeRepository {
	return &DashboardRecoveryCodeRepository{pool: pool}
}

func (r *DashboardRecoveryCodeRepository) InsertBatch(ctx context.Context, codes []domain.DashboardRecoveryCode) error {
	batch := &pgx.Batch{}
	for _, code := range codes {
		batch.Queue(`INSERT INTO dashboard_recovery_codes (id, user_id, code_hash, consumed_at, created_at) VALUES ($1,$2,$3,$4,$5)`,
			code.ID.Value, code.UserID.Value, code.CodeHash, code.ConsumedAt, code.CreatedAt)
	}
	return r.pool.SendBatch(ctx, batch).Close()
}

func (r *DashboardRecoveryCodeRepository) FindActiveByHash(ctx context.Context, userID domain.DashboardUserID, codeHash []byte) (domain.DashboardRecoveryCode, bool, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, code_hash, consumed_at, created_at FROM dashboard_recovery_codes
		 WHERE user_id=$1 AND code_hash=$2 AND consumed_at IS NULL`, userID.Value, codeHash)
	var c domain.DashboardRecoveryCode
	if err := row.Scan(&c.ID.Value, &c.UserID.Value, &c.CodeHash, &c.ConsumedAt, &c.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.DashboardRecoveryCode{}, false, nil
		}
		return domain.DashboardRecoveryCode{}, false, err
	}
	return c, true, nil
}

func (r *DashboardRecoveryCodeRepository) Consume(ctx context.Context, id domain.DashboardRecoveryCodeID) error {
	_, err := r.pool.Exec(ctx, `UPDATE dashboard_recovery_codes SET consumed_at = now() WHERE id=$1 AND consumed_at IS NULL`, id.Value)
	return err
}

func (r *DashboardRecoveryCodeRepository) DeleteAllForUser(ctx context.Context, userID domain.DashboardUserID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM dashboard_recovery_codes WHERE user_id=$1`, userID.Value)
	return err
}

// --- rate limiter ---

type DashboardRateLimiter struct{ pool *pgxpool.Pool }

func NewDashboardRateLimiter(pool *pgxpool.Pool) *DashboardRateLimiter {
	return &DashboardRateLimiter{pool: pool}
}

// Allow implements a simple fixed-window counter keyed by an opaque hash the caller derives
// (see application.rateLimitKey): if no row exists, or the existing window is older than
// window, it starts a fresh window at attempts=1; otherwise it increments and compares against
// maxAttempts — all in one statement so concurrent requests for the same key serialize on the
// row instead of racing past each other.
func (r *DashboardRateLimiter) Allow(ctx context.Context, key string, maxAttempts int, window time.Duration) (bool, error) {
	keyHash := []byte(key)
	var attempts int
	err := r.pool.QueryRow(ctx, `
		INSERT INTO dashboard_auth_rate_limits (key_hash, attempts, window_start)
		VALUES ($1, 1, now())
		ON CONFLICT (key_hash) DO UPDATE SET
			attempts = CASE WHEN dashboard_auth_rate_limits.window_start < now() - make_interval(secs => $2) THEN 1 ELSE dashboard_auth_rate_limits.attempts + 1 END,
			window_start = CASE WHEN dashboard_auth_rate_limits.window_start < now() - make_interval(secs => $2) THEN now() ELSE dashboard_auth_rate_limits.window_start END
		RETURNING attempts`,
		keyHash, window.Seconds()).Scan(&attempts)
	if err != nil {
		return false, err
	}
	return attempts <= maxAttempts, nil
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
