package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

const (
	dashboardInviteTTL          = 72 * time.Hour
	dashboardResetTTL           = time.Hour
	dashboardChallengeTTL       = 10 * time.Minute
	dashboardRefreshTTL         = 7 * 24 * time.Hour
	dashboardMaxLoginAttempts   = 5
	dashboardLockDuration       = 15 * time.Minute
	dashboardRateLimitMax       = 10
	dashboardRateLimitWindow    = 15 * time.Minute
	dashboardRecoveryCodeCount  = 8
	dashboardMaxChallengeChecks = 8
)

// DashboardAuthService is the whole human-login surface for prelo-dashboard — one service,
// same method grouping as messaging-core's DashboardAuthService.java, ported to Go rather than
// redesigned — plus user management (ListUsers/ChangeRole), since both need the same
// DashboardUserRepository and Invite/issueSession machinery. See domain.DashboardRole for the
// ADMIN/OPERATOR distinction.
type DashboardAuthService struct {
	users         DashboardUserRepository
	tokens        DashboardAuthTokenRepository
	recoveryCodes DashboardRecoveryCodeRepository
	rateLimiter   DashboardRateLimiter
	mailer        DashboardMailer
	mfaCipher     DashboardMfaCipher
	totp          DashboardTotpProvider
	hasher        DashboardPasswordHasher
	sessions      DashboardSessionIssuer
	mobile        MobileSessionRepository
}

func NewDashboardAuthService(users DashboardUserRepository, tokens DashboardAuthTokenRepository, recoveryCodes DashboardRecoveryCodeRepository,
	rateLimiter DashboardRateLimiter, mailer DashboardMailer, mfaCipher DashboardMfaCipher, totp DashboardTotpProvider,
	hasher DashboardPasswordHasher, sessions DashboardSessionIssuer) *DashboardAuthService {
	return &DashboardAuthService{users: users, tokens: tokens, recoveryCodes: recoveryCodes, rateLimiter: rateLimiter,
		mailer: mailer, mfaCipher: mfaCipher, totp: totp, hasher: hasher, sessions: sessions}
}

type DashboardChallenge struct {
	Challenge string
	NextStep  string // TOTP_SETUP_REQUIRED | TOTP_REQUIRED
}

type DashboardTotpSetup struct {
	Secret     string
	OtpauthURI string
}

type DashboardSession struct {
	AccessToken   string
	ExpiresIn     int
	RefreshToken  string
	RecoveryCodes []string
}

// Invite creates the account row and emails an activation link. Used two ways: the very first
// user, gated by an admin token at the HTTP layer (there is no logged-in user yet to authorize
// this — same bootstrap shape as messaging-core's DashboardBootstrapController), and every
// subsequent one, gated by an ADMIN dashboard session's users:manage scope instead.
func (s *DashboardAuthService) Invite(ctx context.Context, email string, role domain.DashboardRole) error {
	email = normalizeEmail(email)
	if _, exists, err := s.users.FindByEmail(ctx, email); err != nil {
		return err
	} else if exists {
		return ErrDashboardUserAlreadyExists
	}
	user, err := domain.NewDashboardUser(email, role)
	if err != nil {
		return err
	}
	if err := s.users.Insert(ctx, user); err != nil {
		return err
	}
	return s.issueAndSend(ctx, user, domain.DashboardTokenInvite, dashboardInviteTTL, s.mailer.SendActivation)
}

// ListUsers backs the Usuários page — every dashboard account, for an ADMIN to review and manage.
func (s *DashboardAuthService) ListUsers(ctx context.Context) ([]domain.DashboardUser, error) {
	return s.users.List(ctx)
}

// ChangeRole is the only way a user's role changes after invitation. actorID is whoever is
// making the request (from the dashboard session) — refused against their own account so an
// admin can never accidentally lock themselves out, without needing to track how many other
// admins remain.
func (s *DashboardAuthService) ChangeRole(ctx context.Context, actorID, targetID domain.DashboardUserID, role domain.DashboardRole) (domain.DashboardUser, error) {
	if !role.Valid() {
		return domain.DashboardUser{}, &domain.ValidationError{Message: "role must be ADMIN or OPERATOR"}
	}
	if actorID == targetID {
		return domain.DashboardUser{}, ErrDashboardCannotChangeOwnRole
	}
	user, err := s.users.FindByID(ctx, targetID)
	if err != nil {
		return domain.DashboardUser{}, err
	}
	user = user.WithRole(role)
	if err := s.users.Update(ctx, user); err != nil {
		return domain.DashboardUser{}, err
	}
	s.revokeMobileSessions(ctx, user.ID, "role_changed")
	return user, nil
}

// Activate consumes the invite token and sets the account's password. TOTP is configured
// separately, on first login (SetupTotp/ConfirmTotp) — this only proves the person controls the
// invited email address and picks a password.
func (s *DashboardAuthService) Activate(ctx context.Context, rawToken, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	token, err := s.consumeActive(ctx, domain.DashboardTokenInvite, rawToken)
	if err != nil {
		return err
	}
	user, err := s.users.FindByID(ctx, token.UserID)
	if err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	return s.users.Update(ctx, user.Activated(hash, time.Now().UTC()))
}

// Login checks the password and, on success, always issues a CHALLENGE token — TOTP is
// mandatory here, there is no password-only session. nextStep tells the frontend whether to
// show the TOTP QR setup (first login) or just ask for a code.
func (s *DashboardAuthService) Login(ctx context.Context, email, password string) (DashboardChallenge, error) {
	email = normalizeEmail(email)
	allowed, err := s.rateLimiter.Allow(ctx, rateLimitKey("login", email), dashboardRateLimitMax, dashboardRateLimitWindow)
	if err != nil {
		return DashboardChallenge{}, err
	}
	if !allowed {
		return DashboardChallenge{}, ErrDashboardRateLimited
	}
	user, exists, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return DashboardChallenge{}, err
	}
	now := time.Now().UTC()
	// A nonexistent account runs the same bcrypt-shaped failure path (via a fixed dummy hash)
	// so timing never reveals whether the email is registered.
	if !exists {
		s.hasher.Matches(password, dummyBcryptHash)
		return DashboardChallenge{}, ErrDashboardInvalidCredentials
	}
	if user.Locked(now) {
		return DashboardChallenge{}, ErrDashboardAccountLocked
	}
	if user.ActivatedAt == nil || !s.hasher.Matches(password, user.PasswordHash) {
		if user.ActivatedAt != nil {
			_ = s.users.Update(ctx, user.WithFailedLogin(now, dashboardMaxLoginAttempts, dashboardLockDuration))
		}
		return DashboardChallenge{}, ErrDashboardInvalidCredentials
	}
	if err := s.users.Update(ctx, user.WithSuccessfulLogin()); err != nil {
		return DashboardChallenge{}, err
	}
	raw, hash := newRawToken()
	if err := s.tokens.Insert(ctx, domain.NewDashboardAuthToken(user.ID, hash, domain.DashboardTokenChallenge, dashboardChallengeTTL)); err != nil {
		return DashboardChallenge{}, err
	}
	nextStep := "TOTP_REQUIRED"
	if !user.TOTPEnabled {
		nextStep = "TOTP_SETUP_REQUIRED"
	}
	return DashboardChallenge{Challenge: raw, NextStep: nextStep}, nil
}

// SetupTotp generates a fresh secret and stores it (unconfirmed — TOTPEnabled stays false)
// against the user behind the given login challenge. Safe to call more than once before
// confirming; each call replaces the pending secret.
func (s *DashboardAuthService) SetupTotp(ctx context.Context, challenge string) (DashboardTotpSetup, error) {
	user, err := s.userForActiveToken(ctx, domain.DashboardTokenChallenge, challenge)
	if err != nil {
		return DashboardTotpSetup{}, err
	}
	if user.TOTPEnabled {
		return DashboardTotpSetup{}, ErrDashboardTotpAlreadyEnabled
	}
	secret, uri, err := s.totp.GenerateSecret(user.Email)
	if err != nil {
		return DashboardTotpSetup{}, err
	}
	encrypted, err := s.mfaCipher.Encrypt([]byte(secret), aad(user.ID))
	if err != nil {
		return DashboardTotpSetup{}, err
	}
	if err := s.users.Update(ctx, user.WithPendingTOTPSecret(encrypted)); err != nil {
		return DashboardTotpSetup{}, err
	}
	return DashboardTotpSetup{Secret: secret, OtpauthURI: uri}, nil
}

// ConfirmTotp proves the user actually captured the secret (a valid code from it), enables
// TOTP, mints a fresh set of recovery codes (replacing any from a previous setup), consumes the
// login challenge, and returns a full session — same as VerifyTotp from here on.
func (s *DashboardAuthService) ConfirmTotp(ctx context.Context, challenge, code string) (DashboardSession, error) {
	user, token, err := s.userAndTokenForActiveChallenge(ctx, challenge)
	if err != nil {
		return DashboardSession{}, err
	}
	if user.TOTPSecretEncrypted == nil {
		return DashboardSession{}, ErrDashboardInvalidCode
	}
	secret, err := s.mfaCipher.Decrypt(user.TOTPSecretEncrypted, aad(user.ID))
	if err != nil || !s.totp.Validate(code, string(secret)) {
		return DashboardSession{}, s.rejectChallenge(ctx, token)
	}
	if err := s.users.Update(ctx, user.TOTPConfirmed()); err != nil {
		return DashboardSession{}, err
	}
	if err := s.recoveryCodes.DeleteAllForUser(ctx, user.ID); err != nil {
		return DashboardSession{}, err
	}
	codes, err := s.mintRecoveryCodes(ctx, user.ID)
	if err != nil {
		return DashboardSession{}, err
	}
	if err := s.tokens.Consume(ctx, token.ID); err != nil {
		return DashboardSession{}, err
	}
	session, err := s.issueSession(ctx, user.ID, user.Role)
	if err != nil {
		return DashboardSession{}, err
	}
	session.RecoveryCodes = codes
	return session, nil
}

// VerifyTotp is the normal (non-setup) second factor: a 6-digit code, or one of the 8 recovery
// codes if the authenticator app is unavailable.
func (s *DashboardAuthService) VerifyTotp(ctx context.Context, challenge, code string) (DashboardSession, error) {
	user, err := s.passSecondFactor(ctx, challenge, code)
	if err != nil {
		return DashboardSession{}, err
	}
	return s.issueSession(ctx, user.ID, user.Role)
}

// passSecondFactor checks a login challenge's TOTP (or recovery) code and consumes the challenge —
// shared by the web session (VerifyTotp) and the app session (MobileVerify, PR-2).
func (s *DashboardAuthService) passSecondFactor(ctx context.Context, challenge, code string) (domain.DashboardUser, error) {
	user, token, err := s.userAndTokenForActiveChallenge(ctx, challenge)
	if err != nil {
		return domain.DashboardUser{}, err
	}
	if !user.TOTPEnabled || user.TOTPSecretEncrypted == nil {
		return domain.DashboardUser{}, ErrDashboardInvalidCode
	}
	valid := s.validTotp(user, code)
	if !valid {
		valid, err = s.tryRecoveryCode(ctx, user.ID, code)
		if err != nil {
			return domain.DashboardUser{}, err
		}
	}
	if !valid {
		return domain.DashboardUser{}, s.rejectChallenge(ctx, token)
	}
	if err := s.tokens.Consume(ctx, token.ID); err != nil {
		return domain.DashboardUser{}, err
	}
	return user, nil
}

func (s *DashboardAuthService) validTotp(user domain.DashboardUser, code string) bool {
	if !user.TOTPEnabled || user.TOTPSecretEncrypted == nil {
		return false
	}
	secret, err := s.mfaCipher.Decrypt(user.TOTPSecretEncrypted, aad(user.ID))
	return err == nil && s.totp.Validate(code, string(secret))
}

// --- PR-2: app sessions (docs/integracoes/sessao-mobile.md) ---

// SetMobileSessions enables the Work Control app's sessions.
func (s *DashboardAuthService) SetMobileSessions(mobile MobileSessionRepository) { s.mobile = mobile }

type MobileSessionTokens struct {
	AccessToken      string
	ExpiresIn        int
	RefreshToken     string
	RefreshExpiresAt time.Time
	SessionID        uuid.UUID
}

type MobileDevice struct {
	ID       uuid.UUID
	Name     string
	Platform string
}

// MobileVerify finishes an app login: same challenge and second factor as the web, but the refresh
// token comes back in the body, bound to one device. Signing in again on the same device replaces
// that device's previous session.
func (s *DashboardAuthService) MobileVerify(ctx context.Context, challenge, code string, device MobileDevice) (MobileSessionTokens, error) {
	if s.mobile == nil {
		return MobileSessionTokens{}, ErrMobileSessionNotFound
	}
	now := time.Now().UTC()
	session, err := domain.NewMobileSession(domain.DashboardUserID{}, device.ID, device.Name, device.Platform, now)
	if err != nil {
		return MobileSessionTokens{}, err // validate the device before spending the challenge
	}
	user, err := s.passSecondFactor(ctx, challenge, code)
	if err != nil {
		return MobileSessionTokens{}, err
	}
	session.UserID = user.ID
	if existing, err := s.mobile.ListActiveByUser(ctx, user.ID, now); err == nil {
		for _, old := range existing {
			if old.DeviceID == device.ID {
				_ = s.mobile.Revoke(ctx, old.ID, "replaced")
			}
		}
	}
	raw, hash := newRawToken()
	if err := s.mobile.Insert(ctx, session, hash); err != nil {
		return MobileSessionTokens{}, err
	}
	return s.mobileTokens(user, session, raw)
}

// MobileRefresh rotates the device's refresh token. Reusing a spent one revokes the session.
func (s *DashboardAuthService) MobileRefresh(ctx context.Context, rawRefresh string, deviceID uuid.UUID) (MobileSessionTokens, error) {
	if s.mobile == nil || strings.TrimSpace(rawRefresh) == "" {
		return MobileSessionTokens{}, ErrMobileRefreshInvalid
	}
	raw, hash := newRawToken()
	session, err := s.mobile.Rotate(ctx, hashToken(rawRefresh), hash, deviceID, time.Now().UTC())
	if err != nil {
		return MobileSessionTokens{}, err
	}
	user, err := s.users.FindByID(ctx, session.UserID)
	if err != nil {
		return MobileSessionTokens{}, err
	}
	return s.mobileTokens(user, session, raw)
}

// MobileLogout ends the device's session. Unknown tokens are ignored (logout is idempotent).
func (s *DashboardAuthService) MobileLogout(ctx context.Context, rawRefresh string, deviceID uuid.UUID) {
	if s.mobile == nil || strings.TrimSpace(rawRefresh) == "" {
		return
	}
	session, err := s.mobile.FindByTokenHash(ctx, hashToken(rawRefresh))
	if err == nil && session.DeviceID == deviceID {
		_ = s.mobile.Revoke(ctx, session.ID, "logout")
	}
}

func (s *DashboardAuthService) mobileTokens(user domain.DashboardUser, session domain.MobileSession, rawRefresh string) (MobileSessionTokens, error) {
	access, expiresIn, err := s.sessions.IssueSessionAccessToken(user.ID.String(), user.Role, session.ID.String())
	if err != nil {
		return MobileSessionTokens{}, err
	}
	return MobileSessionTokens{AccessToken: access, ExpiresIn: expiresIn, RefreshToken: rawRefresh,
		RefreshExpiresAt: session.IdleExpiresAt, SessionID: session.ID}, nil
}

// ConfirmStepUp (contratos G9) checks a fresh TOTP code for an app session (recovery codes don't
// count here) and records it, so HIGH-risk approvals in the next few minutes don't ask again.
func (s *DashboardAuthService) ConfirmStepUp(ctx context.Context, sessionID uuid.UUID, userID domain.DashboardUserID, code string) error {
	if s.mobile == nil {
		return ErrStepUpRequired
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if !s.validTotp(user, strings.TrimSpace(code)) {
		return ErrStepUpRequired
	}
	return s.mobile.TouchTotp(ctx, sessionID, time.Now().UTC())
}

// revokeMobileSessions ends every app session of a user whose trust changed (role, password).
func (s *DashboardAuthService) revokeMobileSessions(ctx context.Context, userID domain.DashboardUserID, reason string) {
	if s.mobile != nil {
		_, _ = s.mobile.RevokeAllForUser(ctx, userID, reason)
	}
}

// Refresh exchanges a still-valid refresh token (from the HttpOnly cookie) for a new access
// token, rotating the refresh token itself so a stolen cookie has a shrinking window of use.
func (s *DashboardAuthService) Refresh(ctx context.Context, rawRefreshToken string) (DashboardSession, error) {
	token, err := s.consumeActive(ctx, domain.DashboardTokenRefresh, rawRefreshToken)
	if err != nil {
		return DashboardSession{}, err
	}
	user, err := s.users.FindByID(ctx, token.UserID)
	if err != nil {
		return DashboardSession{}, err
	}
	return s.issueSession(ctx, token.UserID, user.Role)
}

func (s *DashboardAuthService) Logout(ctx context.Context, rawRefreshToken string) {
	if rawRefreshToken == "" {
		return
	}
	hash := hashToken(rawRefreshToken)
	if token, ok, err := s.tokens.FindActiveByHash(ctx, domain.DashboardTokenRefresh, hash); err == nil && ok {
		_ = s.tokens.Consume(ctx, token.ID)
	}
}

func (s *DashboardAuthService) RequestPasswordReset(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	allowed, err := s.rateLimiter.Allow(ctx, rateLimitKey("reset", email), dashboardRateLimitMax, dashboardRateLimitWindow)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrDashboardRateLimited
	}
	user, exists, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return err
	}
	if !exists {
		return nil // never reveal whether the email is registered
	}
	return s.issueAndSend(ctx, user, domain.DashboardTokenReset, dashboardResetTTL, s.mailer.SendPasswordReset)
}

func (s *DashboardAuthService) ResetPassword(ctx context.Context, rawToken, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	token, err := s.consumeActive(ctx, domain.DashboardTokenReset, rawToken)
	if err != nil {
		return err
	}
	user, err := s.users.FindByID(ctx, token.UserID)
	if err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	if err := s.users.Update(ctx, user.WithPassword(hash).WithSuccessfulLogin()); err != nil {
		return err
	}
	s.revokeMobileSessions(ctx, user.ID, "password_reset")
	return nil
}

// --- helpers ---

func (s *DashboardAuthService) issueAndSend(ctx context.Context, user domain.DashboardUser, purpose domain.DashboardTokenPurpose, ttl time.Duration, send func(to, token string) error) error {
	raw, hash := newRawToken()
	if err := s.tokens.Insert(ctx, domain.NewDashboardAuthToken(user.ID, hash, purpose, ttl)); err != nil {
		return err
	}
	return send(user.Email, raw)
}

func (s *DashboardAuthService) consumeActive(ctx context.Context, purpose domain.DashboardTokenPurpose, raw string) (domain.DashboardAuthToken, error) {
	token, ok, err := s.tokens.FindActiveByHash(ctx, purpose, hashToken(raw))
	if err != nil {
		return domain.DashboardAuthToken{}, err
	}
	if !ok {
		return domain.DashboardAuthToken{}, ErrDashboardTokenNotFound
	}
	if err := s.tokens.Consume(ctx, token.ID); err != nil {
		return domain.DashboardAuthToken{}, err
	}
	return token, nil
}

func (s *DashboardAuthService) userForActiveToken(ctx context.Context, purpose domain.DashboardTokenPurpose, raw string) (domain.DashboardUser, error) {
	token, ok, err := s.tokens.FindActiveByHash(ctx, purpose, hashToken(raw))
	if err != nil {
		return domain.DashboardUser{}, err
	}
	if !ok {
		return domain.DashboardUser{}, ErrDashboardTokenNotFound
	}
	return s.users.FindByID(ctx, token.UserID)
}

func (s *DashboardAuthService) userAndTokenForActiveChallenge(ctx context.Context, raw string) (domain.DashboardUser, domain.DashboardAuthToken, error) {
	token, ok, err := s.tokens.FindActiveByHash(ctx, domain.DashboardTokenChallenge, hashToken(raw))
	if err != nil {
		return domain.DashboardUser{}, domain.DashboardAuthToken{}, err
	}
	if !ok {
		return domain.DashboardUser{}, domain.DashboardAuthToken{}, ErrDashboardTokenNotFound
	}
	if token.Attempts >= dashboardMaxChallengeChecks {
		return domain.DashboardUser{}, domain.DashboardAuthToken{}, ErrDashboardTokenLimitExceeded
	}
	user, err := s.users.FindByID(ctx, token.UserID)
	if err != nil {
		return domain.DashboardUser{}, domain.DashboardAuthToken{}, err
	}
	return user, token, nil
}

// rejectChallenge records a failed attempt against the challenge token itself (not the user's
// own lockout counter — a wrong TOTP code mid-login is a separate, tighter-budgeted failure
// mode) and, once dashboardMaxChallengeChecks is reached, the challenge stops being usable at
// all via userAndTokenForActiveChallenge's own check on the next attempt.
func (s *DashboardAuthService) rejectChallenge(ctx context.Context, token domain.DashboardAuthToken) error {
	_, _ = s.tokens.IncrementAttempts(ctx, token.ID)
	return ErrDashboardInvalidCode
}

func (s *DashboardAuthService) tryRecoveryCode(ctx context.Context, userID domain.DashboardUserID, code string) (bool, error) {
	normalized := strings.ToUpper(strings.TrimSpace(code))
	if normalized == "" {
		return false, nil
	}
	record, ok, err := s.recoveryCodes.FindActiveByHash(ctx, userID, hashRecoveryCode(normalized))
	if err != nil || !ok {
		return false, err
	}
	return true, s.recoveryCodes.Consume(ctx, record.ID)
}

func (s *DashboardAuthService) mintRecoveryCodes(ctx context.Context, userID domain.DashboardUserID) ([]string, error) {
	codes := make([]string, 0, dashboardRecoveryCodeCount)
	records := make([]domain.DashboardRecoveryCode, 0, dashboardRecoveryCodeCount)
	for i := 0; i < dashboardRecoveryCodeCount; i++ {
		raw := make([]byte, 6)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		code := strings.ToUpper(hex.EncodeToString(raw))
		codes = append(codes, code)
		records = append(records, domain.NewDashboardRecoveryCode(userID, hashRecoveryCode(code)))
	}
	if err := s.recoveryCodes.InsertBatch(ctx, records); err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *DashboardAuthService) issueSession(ctx context.Context, userID domain.DashboardUserID, role domain.DashboardRole) (DashboardSession, error) {
	access, expiresIn, err := s.sessions.IssueAccessToken(userID.String(), role)
	if err != nil {
		return DashboardSession{}, err
	}
	rawRefresh, refreshHash := newRawToken()
	if err := s.tokens.Insert(ctx, domain.NewDashboardAuthToken(userID, refreshHash, domain.DashboardTokenRefresh, dashboardRefreshTTL)); err != nil {
		return DashboardSession{}, err
	}
	return DashboardSession{AccessToken: access, ExpiresIn: expiresIn, RefreshToken: rawRefresh}, nil
}

func newRawToken() (raw string, hash []byte) {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashToken(raw)
}

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func hashRecoveryCode(code string) []byte { return hashToken(code) }

func aad(userID domain.DashboardUserID) []byte { return []byte(userID.String()) }

func rateLimitKey(scope, email string) string {
	sum := sha256.Sum256([]byte(scope + ":" + email))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func validatePassword(password string) error {
	if len(password) < 12 || len(password) > 64 {
		return &domain.ValidationError{Message: "password must be 12-64 characters"}
	}
	return nil
}

// dummyBcryptHash is checked against on a login attempt for an email that doesn't exist, so the
// response takes the same time either way and never leaks whether the account is registered.
const dummyBcryptHash = "$2a$10$CwTycUXWue0Thq9StjUM0uJ8i8b8k8b8k8b8k8b8k8b8k8b8k8b8k"
