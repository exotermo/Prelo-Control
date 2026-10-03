package domain

import (
	"time"

	"github.com/google/uuid"
)

type DashboardUserID struct{ Value uuid.UUID }

func NewDashboardUserID() DashboardUserID { return DashboardUserID{Value: uuid.New()} }

func (id DashboardUserID) String() string { return id.Value.String() }

// DashboardRole gates which scopes a session gets (see api.dashboardAdminScopes/
// dashboardOperatorScopes) — ADMIN can manage Integrações/Configurações and invite/promote other
// users; OPERATOR runs day-to-day work (tasks, approvals, tools, chat) but cannot touch either.
// Added 2026-10-02; every row from before this existed defaults to ADMIN (migration), so nobody
// already using the dashboard loses access they had.
type DashboardRole string

const (
	DashboardRoleAdmin    DashboardRole = "ADMIN"
	DashboardRoleOperator DashboardRole = "OPERATOR"
)

func (r DashboardRole) Valid() bool {
	return r == DashboardRoleAdmin || r == DashboardRoleOperator
}

// DashboardUser is a human operator of hermes-dashboard. PasswordHash is empty and TOTPEnabled is
// false until Activate sets a password; TOTPSecretEncrypted is set only once TOTP setup is
// confirmed (AES-GCM ciphertext, never the raw secret — see infrastructure/security.MfaCipher).
type DashboardUser struct {
	ID                  DashboardUserID
	Email               string
	PasswordHash        string
	TOTPSecretEncrypted []byte
	TOTPEnabled         bool
	FailedLogins        int
	LockedUntil         *time.Time
	ActivatedAt         *time.Time
	CreatedAt           time.Time
	Role                DashboardRole
}

func NewDashboardUser(email string, role DashboardRole) (DashboardUser, error) {
	if isBlank(email) {
		return DashboardUser{}, &ValidationError{Message: "email is required"}
	}
	if !role.Valid() {
		return DashboardUser{}, &ValidationError{Message: "role must be ADMIN or OPERATOR"}
	}
	return DashboardUser{ID: NewDashboardUserID(), Email: email, CreatedAt: time.Now().UTC(), Role: role}, nil
}

// WithRole is how ChangeRole (user management, ADMIN-only) updates an existing account.
func (u DashboardUser) WithRole(role DashboardRole) DashboardUser {
	u.Role = role
	return u
}

// Locked reports whether the account is currently locked out from login attempts.
func (u DashboardUser) Locked(now time.Time) bool {
	return u.LockedUntil != nil && now.Before(*u.LockedUntil)
}

// WithFailedLogin increments the failure counter and, once it reaches maxAttempts, locks the
// account until now+lockDuration — mirrors DashboardAuthService's lockout policy.
func (u DashboardUser) WithFailedLogin(now time.Time, maxAttempts int, lockDuration time.Duration) DashboardUser {
	u.FailedLogins++
	if u.FailedLogins >= maxAttempts {
		lockedUntil := now.Add(lockDuration)
		u.LockedUntil = &lockedUntil
	}
	return u
}

func (u DashboardUser) WithSuccessfulLogin() DashboardUser {
	u.FailedLogins = 0
	u.LockedUntil = nil
	return u
}

func (u DashboardUser) Activated(passwordHash string, now time.Time) DashboardUser {
	u.PasswordHash = passwordHash
	u.ActivatedAt = &now
	return u
}

func (u DashboardUser) WithPassword(passwordHash string) DashboardUser {
	u.PasswordHash = passwordHash
	return u
}

// WithPendingTOTPSecret stores the encrypted secret generated during setup without enabling
// it — TOTPEnabled only flips once ConfirmTOTP proves the user actually captured the QR code
// (a valid code was produced from it), matching the "prove possession before enabling" rule.
func (u DashboardUser) WithPendingTOTPSecret(secretEncrypted []byte) DashboardUser {
	u.TOTPSecretEncrypted = secretEncrypted
	u.TOTPEnabled = false
	return u
}

func (u DashboardUser) TOTPConfirmed() DashboardUser {
	u.TOTPEnabled = true
	return u
}
