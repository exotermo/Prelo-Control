package application

import (
	"context"
	"errors"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
)

var ErrDashboardUserNotFound = errors.New("dashboard user not found")
var ErrDashboardUserAlreadyExists = errors.New("dashboard user already exists")
var ErrDashboardTokenNotFound = errors.New("dashboard auth token not found or already used")
var ErrDashboardTokenLimitExceeded = errors.New("too many attempts for this token")
var ErrDashboardAccountLocked = errors.New("account is locked, try again later")
var ErrDashboardRateLimited = errors.New("too many attempts, try again later")
var ErrDashboardInvalidCredentials = errors.New("invalid email or password")
var ErrDashboardInvalidCode = errors.New("invalid or expired code")
var ErrDashboardTotpAlreadyEnabled = errors.New("two-factor authentication is already enabled")
var ErrDashboardCannotChangeOwnRole = errors.New("you cannot change your own role")

type DashboardUserRepository interface {
	Insert(ctx context.Context, user domain.DashboardUser) error
	FindByID(ctx context.Context, id domain.DashboardUserID) (domain.DashboardUser, error)
	FindByEmail(ctx context.Context, email string) (domain.DashboardUser, bool, error)
	Update(ctx context.Context, user domain.DashboardUser) error
	// List backs the Usuários page (user management) — every account, newest first. There is no
	// pagination yet, same "read what's there" scope as the Tasks list.
	List(ctx context.Context) ([]domain.DashboardUser, error)
}

// DashboardAuthTokenRepository backs every one-time token: invite/activation, password reset,
// the in-flight TOTP challenge, and refresh sessions (see domain.DashboardTokenPurpose).
type DashboardAuthTokenRepository interface {
	Insert(ctx context.Context, token domain.DashboardAuthToken) error
	FindActiveByHash(ctx context.Context, purpose domain.DashboardTokenPurpose, tokenHash []byte) (domain.DashboardAuthToken, bool, error)
	FindByID(ctx context.Context, id domain.DashboardAuthTokenID) (domain.DashboardAuthToken, error)
	// Consume is a guarded UPDATE (WHERE consumed_at IS NULL) — the same token can never be
	// accepted twice even under concurrent requests.
	Consume(ctx context.Context, id domain.DashboardAuthTokenID) error
	IncrementAttempts(ctx context.Context, id domain.DashboardAuthTokenID) (int, error)
}

type DashboardRecoveryCodeRepository interface {
	InsertBatch(ctx context.Context, codes []domain.DashboardRecoveryCode) error
	FindActiveByHash(ctx context.Context, userID domain.DashboardUserID, codeHash []byte) (domain.DashboardRecoveryCode, bool, error)
	Consume(ctx context.Context, id domain.DashboardRecoveryCodeID) error
	DeleteAllForUser(ctx context.Context, userID domain.DashboardUserID) error
}

// DashboardRateLimiter guards login/reset endpoints by a caller-chosen key (typically a hash of
// email+IP). Allow returns false once the key has exceeded maxAttempts within window; the
// window itself resets lazily once it's older than window, mirroring messaging-core's sliding
// window in dashboard_auth_rate_limits.
type DashboardRateLimiter interface {
	Allow(ctx context.Context, key string, maxAttempts int, window time.Duration) (bool, error)
}

type DashboardMailer interface {
	SendActivation(to, token string) error
	SendPasswordReset(to, token string) error
}

// DashboardMfaCipher and DashboardTotpProvider are narrow ports over
// internal/infrastructure/security's concrete AES-GCM cipher and TOTP provider, so the
// application layer never imports infrastructure directly.
type DashboardMfaCipher interface {
	Encrypt(plaintext []byte, aad []byte) ([]byte, error)
	Decrypt(stored []byte, aad []byte) ([]byte, error)
}

type DashboardTotpProvider interface {
	GenerateSecret(accountName string) (secret string, otpauthURI string, err error)
	Validate(code, secret string) bool
}

type DashboardPasswordHasher interface {
	Hash(password string) (string, error)
	Matches(password, hash string) bool
}

// DashboardSessionIssuer mints the short-lived access JWT handed to the browser — signed with
// the same secret/issuer/audience prelo-core's own JWTAuthMiddleware already validates
// integration/technical tokens against (token_use="dashboard" is simply a third accepted value,
// no new shared secret needed).
type DashboardSessionIssuer interface {
	IssueAccessToken(userID string, role domain.DashboardRole) (token string, expiresInSeconds int, err error)
}
