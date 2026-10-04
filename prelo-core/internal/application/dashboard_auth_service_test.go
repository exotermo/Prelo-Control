package application

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
)

// --- in-memory fakes, same style as invoke_tool_test.go ---

type fakeDashboardUsers struct {
	users map[string]domain.DashboardUser
}

func newFakeDashboardUsers() *fakeDashboardUsers {
	return &fakeDashboardUsers{users: map[string]domain.DashboardUser{}}
}
func (f *fakeDashboardUsers) Insert(_ context.Context, u domain.DashboardUser) error {
	f.users[u.ID.String()] = u
	return nil
}
func (f *fakeDashboardUsers) FindByID(_ context.Context, id domain.DashboardUserID) (domain.DashboardUser, error) {
	u, ok := f.users[id.String()]
	if !ok {
		return domain.DashboardUser{}, ErrDashboardUserNotFound
	}
	return u, nil
}
func (f *fakeDashboardUsers) FindByEmail(_ context.Context, email string) (domain.DashboardUser, bool, error) {
	for _, u := range f.users {
		if u.Email == email {
			return u, true, nil
		}
	}
	return domain.DashboardUser{}, false, nil
}
func (f *fakeDashboardUsers) Update(_ context.Context, u domain.DashboardUser) error {
	f.users[u.ID.String()] = u
	return nil
}
func (f *fakeDashboardUsers) List(_ context.Context) ([]domain.DashboardUser, error) {
	users := make([]domain.DashboardUser, 0, len(f.users))
	for _, u := range f.users {
		users = append(users, u)
	}
	return users, nil
}

type fakeDashboardTokens struct {
	tokens map[string]domain.DashboardAuthToken
}

func newFakeDashboardTokens() *fakeDashboardTokens {
	return &fakeDashboardTokens{tokens: map[string]domain.DashboardAuthToken{}}
}
func (f *fakeDashboardTokens) Insert(_ context.Context, t domain.DashboardAuthToken) error {
	f.tokens[t.ID.String()] = t
	return nil
}
func (f *fakeDashboardTokens) FindActiveByHash(_ context.Context, purpose domain.DashboardTokenPurpose, hash []byte) (domain.DashboardAuthToken, bool, error) {
	now := time.Now().UTC()
	for _, t := range f.tokens {
		if t.Purpose == purpose && string(t.TokenHash) == string(hash) && t.Active(now) {
			return t, true, nil
		}
	}
	return domain.DashboardAuthToken{}, false, nil
}
func (f *fakeDashboardTokens) FindByID(_ context.Context, id domain.DashboardAuthTokenID) (domain.DashboardAuthToken, error) {
	t, ok := f.tokens[id.String()]
	if !ok {
		return domain.DashboardAuthToken{}, ErrDashboardTokenNotFound
	}
	return t, nil
}
func (f *fakeDashboardTokens) Consume(_ context.Context, id domain.DashboardAuthTokenID) error {
	t, ok := f.tokens[id.String()]
	if !ok || t.ConsumedAt != nil {
		return ErrDashboardTokenNotFound
	}
	now := time.Now().UTC()
	t.ConsumedAt = &now
	f.tokens[id.String()] = t
	return nil
}
func (f *fakeDashboardTokens) IncrementAttempts(_ context.Context, id domain.DashboardAuthTokenID) (int, error) {
	t := f.tokens[id.String()]
	t.Attempts++
	f.tokens[id.String()] = t
	return t.Attempts, nil
}

type fakeDashboardRecoveryCodes struct {
	codes map[string]domain.DashboardRecoveryCode
}

func newFakeDashboardRecoveryCodes() *fakeDashboardRecoveryCodes {
	return &fakeDashboardRecoveryCodes{codes: map[string]domain.DashboardRecoveryCode{}}
}
func (f *fakeDashboardRecoveryCodes) InsertBatch(_ context.Context, codes []domain.DashboardRecoveryCode) error {
	for _, c := range codes {
		f.codes[c.ID.String()] = c
	}
	return nil
}
func (f *fakeDashboardRecoveryCodes) FindActiveByHash(_ context.Context, userID domain.DashboardUserID, hash []byte) (domain.DashboardRecoveryCode, bool, error) {
	for _, c := range f.codes {
		if c.UserID == userID && string(c.CodeHash) == string(hash) && c.ConsumedAt == nil {
			return c, true, nil
		}
	}
	return domain.DashboardRecoveryCode{}, false, nil
}
func (f *fakeDashboardRecoveryCodes) Consume(_ context.Context, id domain.DashboardRecoveryCodeID) error {
	c := f.codes[id.String()]
	now := time.Now().UTC()
	c.ConsumedAt = &now
	f.codes[id.String()] = c
	return nil
}
func (f *fakeDashboardRecoveryCodes) DeleteAllForUser(_ context.Context, userID domain.DashboardUserID) error {
	for k, c := range f.codes {
		if c.UserID == userID {
			delete(f.codes, k)
		}
	}
	return nil
}

type fakeDashboardRateLimiter struct{}

func (fakeDashboardRateLimiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return true, nil
}

type fakeDashboardMailer struct{ lastActivationToken, lastResetToken string }

func (m *fakeDashboardMailer) SendActivation(_, token string) error {
	m.lastActivationToken = token
	return nil
}
func (m *fakeDashboardMailer) SendPasswordReset(_, token string) error {
	m.lastResetToken = token
	return nil
}

// fakeMfaCipher: XOR "encryption" good enough to prove the plumbing without pulling in real AES.
type fakeMfaCipher struct{}

func (fakeMfaCipher) Encrypt(plaintext []byte, aad []byte) ([]byte, error) {
	return append([]byte{}, plaintext...), nil
}
func (fakeMfaCipher) Decrypt(stored []byte, aad []byte) ([]byte, error) {
	return append([]byte{}, stored...), nil
}

type fakeTotpProvider struct{ validCode string }

func (p *fakeTotpProvider) GenerateSecret(accountName string) (string, string, error) {
	return "SECRETSECRET", "otpauth://totp/test", nil
}
func (p *fakeTotpProvider) Validate(code, secret string) bool { return code == p.validCode }

type fakePasswordHasher struct{}

func (fakePasswordHasher) Hash(password string) (string, error) { return "hashed:" + password, nil }
func (fakePasswordHasher) Matches(password, hash string) bool   { return hash == "hashed:"+password }

type fakeSessionIssuer struct{}

func (fakeSessionIssuer) IssueAccessToken(userID string, role domain.DashboardRole) (string, int, error) {
	return "access-for-" + userID + "-" + string(role), 900, nil
}

func (fakeSessionIssuer) IssueSessionAccessToken(userID string, role domain.DashboardRole, sessionID string) (string, int, error) {
	return "access-for-" + userID + "-" + string(role) + "-sid-" + sessionID, 900, nil
}

func setupDashboardAuth(totpCode string) (*DashboardAuthService, *fakeDashboardUsers, *fakeDashboardMailer) {
	users := newFakeDashboardUsers()
	tokens := newFakeDashboardTokens()
	recovery := newFakeDashboardRecoveryCodes()
	mailer := &fakeDashboardMailer{}
	svc := NewDashboardAuthService(users, tokens, recovery, fakeDashboardRateLimiter{}, mailer, fakeMfaCipher{},
		&fakeTotpProvider{validCode: totpCode}, fakePasswordHasher{}, fakeSessionIssuer{})
	return svc, users, mailer
}

const testPassword = "correct horse battery staple"

func TestDashboardAuth_FullFirstLoginFlow_IssuesSessionAndRecoveryCodes(t *testing.T) {
	ctx := context.Background()
	svc, _, mailer := setupDashboardAuth("123456")

	if err := svc.Invite(ctx, "owner@example.com", domain.DashboardRoleAdmin); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if mailer.lastActivationToken == "" {
		t.Fatal("expected an activation token to be emailed")
	}
	if err := svc.Activate(ctx, mailer.lastActivationToken, testPassword); err != nil {
		t.Fatalf("activate: %v", err)
	}

	challenge, err := svc.Login(ctx, "owner@example.com", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if challenge.NextStep != "TOTP_SETUP_REQUIRED" {
		t.Fatalf("expected TOTP_SETUP_REQUIRED for a first login, got %s", challenge.NextStep)
	}

	setup, err := svc.SetupTotp(ctx, challenge.Challenge)
	if err != nil || setup.Secret == "" {
		t.Fatalf("setup totp: %v", err)
	}

	session, err := svc.ConfirmTotp(ctx, challenge.Challenge, "123456")
	if err != nil {
		t.Fatalf("confirm totp: %v", err)
	}
	if session.AccessToken == "" || len(session.RecoveryCodes) != dashboardRecoveryCodeCount {
		t.Fatalf("expected an access token and %d recovery codes, got %+v", dashboardRecoveryCodeCount, session)
	}
	if session.RefreshToken == "" {
		t.Fatal("expected a refresh token")
	}

	// The login challenge must not be usable a second time.
	if _, err := svc.ConfirmTotp(ctx, challenge.Challenge, "123456"); err == nil {
		t.Fatal("expected the consumed challenge to be rejected on reuse")
	}
}

func TestDashboardAuth_SecondLogin_RequiresExistingTotpNotSetup(t *testing.T) {
	ctx := context.Background()
	svc, _, mailer := setupDashboardAuth("654321")
	_ = svc.Invite(ctx, "owner@example.com", domain.DashboardRoleAdmin)
	_ = svc.Activate(ctx, mailer.lastActivationToken, testPassword)
	first, _ := svc.Login(ctx, "owner@example.com", testPassword)
	_, _ = svc.SetupTotp(ctx, first.Challenge)
	if _, err := svc.ConfirmTotp(ctx, first.Challenge, "654321"); err != nil {
		t.Fatalf("confirm totp: %v", err)
	}

	second, err := svc.Login(ctx, "owner@example.com", testPassword)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if second.NextStep != "TOTP_REQUIRED" {
		t.Fatalf("expected TOTP_REQUIRED once TOTP is enabled, got %s", second.NextStep)
	}
	if _, err := svc.VerifyTotp(ctx, second.Challenge, "654321"); err != nil {
		t.Fatalf("verify totp: %v", err)
	}
}

func TestDashboardAuth_WrongPassword_LocksAccountAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	svc, users, mailer := setupDashboardAuth("000000")
	_ = svc.Invite(ctx, "owner@example.com", domain.DashboardRoleAdmin)
	_ = svc.Activate(ctx, mailer.lastActivationToken, testPassword)

	for i := 0; i < dashboardMaxLoginAttempts; i++ {
		if _, err := svc.Login(ctx, "owner@example.com", "wrong password"); err == nil {
			t.Fatal("expected invalid credentials")
		}
	}
	user, _, _ := users.FindByEmail(ctx, "owner@example.com")
	if !user.Locked(time.Now().UTC()) {
		t.Fatal("expected the account to be locked after max failed attempts")
	}
	if _, err := svc.Login(ctx, "owner@example.com", testPassword); err != ErrDashboardAccountLocked {
		t.Fatalf("expected ErrDashboardAccountLocked even with the correct password, got %v", err)
	}
}

func TestDashboardAuth_WrongTotpCode_DoesNotIssueASession(t *testing.T) {
	ctx := context.Background()
	svc, _, mailer := setupDashboardAuth("111111")
	_ = svc.Invite(ctx, "owner@example.com", domain.DashboardRoleAdmin)
	_ = svc.Activate(ctx, mailer.lastActivationToken, testPassword)
	challenge, _ := svc.Login(ctx, "owner@example.com", testPassword)
	_, _ = svc.SetupTotp(ctx, challenge.Challenge)

	if _, err := svc.ConfirmTotp(ctx, challenge.Challenge, "999999"); err != ErrDashboardInvalidCode {
		t.Fatalf("expected ErrDashboardInvalidCode, got %v", err)
	}
}

func TestDashboardAuth_RecoveryCode_WorksOnceThenIsConsumed(t *testing.T) {
	ctx := context.Background()
	svc, _, mailer := setupDashboardAuth("222222")
	_ = svc.Invite(ctx, "owner@example.com", domain.DashboardRoleAdmin)
	_ = svc.Activate(ctx, mailer.lastActivationToken, testPassword)
	first, _ := svc.Login(ctx, "owner@example.com", testPassword)
	_, _ = svc.SetupTotp(ctx, first.Challenge)
	session, err := svc.ConfirmTotp(ctx, first.Challenge, "222222")
	if err != nil {
		t.Fatalf("confirm totp: %v", err)
	}
	code := session.RecoveryCodes[0]

	second, _ := svc.Login(ctx, "owner@example.com", testPassword)
	if _, err := svc.VerifyTotp(ctx, second.Challenge, code); err != nil {
		t.Fatalf("expected the recovery code to work, got %v", err)
	}

	third, _ := svc.Login(ctx, "owner@example.com", testPassword)
	if _, err := svc.VerifyTotp(ctx, third.Challenge, code); err != ErrDashboardInvalidCode {
		t.Fatalf("expected the already-used recovery code to be rejected, got %v", err)
	}
}

func TestDashboardAuth_PasswordResetToken_IsSingleUse(t *testing.T) {
	ctx := context.Background()
	svc, _, mailer := setupDashboardAuth("333333")
	_ = svc.Invite(ctx, "owner@example.com", domain.DashboardRoleAdmin)
	_ = svc.Activate(ctx, mailer.lastActivationToken, testPassword)

	if err := svc.RequestPasswordReset(ctx, "owner@example.com"); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	resetToken := mailer.lastResetToken
	if err := svc.ResetPassword(ctx, resetToken, "a brand new password!!"); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if err := svc.ResetPassword(ctx, resetToken, "another new password!!"); err == nil {
		t.Fatal("expected the reset token to be rejected on reuse")
	}
	if _, err := svc.Login(ctx, "owner@example.com", "a brand new password!!"); err != nil {
		t.Fatalf("expected login with the new password to succeed, got %v", err)
	}
}

func TestDashboardAuth_RefreshAndLogout(t *testing.T) {
	ctx := context.Background()
	svc, _, mailer := setupDashboardAuth("444444")
	_ = svc.Invite(ctx, "owner@example.com", domain.DashboardRoleAdmin)
	_ = svc.Activate(ctx, mailer.lastActivationToken, testPassword)
	challenge, _ := svc.Login(ctx, "owner@example.com", testPassword)
	_, _ = svc.SetupTotp(ctx, challenge.Challenge)
	session, _ := svc.ConfirmTotp(ctx, challenge.Challenge, "444444")

	refreshed, err := svc.Refresh(ctx, session.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.AccessToken == "" {
		t.Fatal("expected a fresh access token")
	}
	// Refresh rotates the token: the old one must no longer work.
	if _, err := svc.Refresh(ctx, session.RefreshToken); err == nil {
		t.Fatal("expected the rotated-out refresh token to be rejected")
	}

	svc.Logout(ctx, refreshed.RefreshToken)
	if _, err := svc.Refresh(ctx, refreshed.RefreshToken); err == nil {
		t.Fatal("expected the logged-out refresh token to be rejected")
	}
}

func TestDashboardAuth_InvitingTheSameEmailTwice_Fails(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := setupDashboardAuth("555555")
	if err := svc.Invite(ctx, "dup@example.com", domain.DashboardRoleAdmin); err != nil {
		t.Fatalf("first invite: %v", err)
	}
	if err := svc.Invite(ctx, "dup@example.com", domain.DashboardRoleAdmin); err != ErrDashboardUserAlreadyExists {
		t.Fatalf("expected ErrDashboardUserAlreadyExists, got %v", err)
	}
}

func TestDashboardAuth_InviteDefaultsToTheGivenRole(t *testing.T) {
	ctx := context.Background()
	svc, users, _ := setupDashboardAuth("555555")
	if err := svc.Invite(ctx, "operator@example.com", domain.DashboardRoleOperator); err != nil {
		t.Fatalf("invite: %v", err)
	}
	user, _, err := users.FindByEmail(ctx, "operator@example.com")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if user.Role != domain.DashboardRoleOperator {
		t.Fatalf("expected OPERATOR, got %q", user.Role)
	}
}

func TestDashboardAuth_ChangeRole_RefusesActingOnYourOwnAccount(t *testing.T) {
	ctx := context.Background()
	svc, users, _ := setupDashboardAuth("555555")
	admin, _ := domain.NewDashboardUser("admin@example.com", domain.DashboardRoleAdmin)
	_ = users.Insert(ctx, admin)

	if _, err := svc.ChangeRole(ctx, admin.ID, admin.ID, domain.DashboardRoleOperator); err != ErrDashboardCannotChangeOwnRole {
		t.Fatalf("expected ErrDashboardCannotChangeOwnRole, got %v", err)
	}
}

func TestDashboardAuth_ChangeRole_PromotesAnotherUser(t *testing.T) {
	ctx := context.Background()
	svc, users, _ := setupDashboardAuth("555555")
	admin, _ := domain.NewDashboardUser("admin@example.com", domain.DashboardRoleAdmin)
	_ = users.Insert(ctx, admin)
	operator, _ := domain.NewDashboardUser("operator@example.com", domain.DashboardRoleOperator)
	_ = users.Insert(ctx, operator)

	updated, err := svc.ChangeRole(ctx, admin.ID, operator.ID, domain.DashboardRoleAdmin)
	if err != nil {
		t.Fatalf("change role: %v", err)
	}
	if updated.Role != domain.DashboardRoleAdmin {
		t.Fatalf("expected ADMIN, got %q", updated.Role)
	}
}

func TestDashboardAuth_ListUsers_ReturnsEveryAccount(t *testing.T) {
	ctx := context.Background()
	svc, users, _ := setupDashboardAuth("555555")
	a, _ := domain.NewDashboardUser("a@example.com", domain.DashboardRoleAdmin)
	b, _ := domain.NewDashboardUser("b@example.com", domain.DashboardRoleOperator)
	_ = users.Insert(ctx, a)
	_ = users.Insert(ctx, b)

	list, err := svc.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 users, got %d", len(list))
	}
}

func TestHashToken_IsDeterministicSHA256(t *testing.T) {
	sum := sha256.Sum256([]byte("abc"))
	if got := hashToken("abc"); string(got) != string(sum[:]) {
		t.Fatal("hashToken must be plain SHA-256 of the raw token")
	}
}
