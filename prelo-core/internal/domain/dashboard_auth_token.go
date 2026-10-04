package domain

import (
	"time"

	"github.com/google/uuid"
)

type DashboardAuthTokenID struct{ Value uuid.UUID }

func NewDashboardAuthTokenID() DashboardAuthTokenID { return DashboardAuthTokenID{Value: uuid.New()} }

func (id DashboardAuthTokenID) String() string { return id.Value.String() }

type DashboardTokenPurpose string

const (
	DashboardTokenInvite    DashboardTokenPurpose = "INVITE"
	DashboardTokenReset     DashboardTokenPurpose = "RESET"
	DashboardTokenChallenge DashboardTokenPurpose = "CHALLENGE"
	DashboardTokenRefresh   DashboardTokenPurpose = "REFRESH"
)

// DashboardAuthToken is the audit/storage record for every one-time token issued: activation
// links, password-reset links, an in-flight login's TOTP challenge, and refresh sessions all
// share this shape. Only TokenHash (SHA-256 of the raw token) is ever persisted — the raw token
// exists only in the email link or the response body, never in the database.
type DashboardAuthToken struct {
	ID         DashboardAuthTokenID
	UserID     DashboardUserID
	TokenHash  []byte
	Purpose    DashboardTokenPurpose
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	Attempts   int
	CreatedAt  time.Time
}

func NewDashboardAuthToken(userID DashboardUserID, tokenHash []byte, purpose DashboardTokenPurpose, ttl time.Duration) DashboardAuthToken {
	now := time.Now().UTC()
	return DashboardAuthToken{
		ID: NewDashboardAuthTokenID(), UserID: userID, TokenHash: tokenHash, Purpose: purpose,
		ExpiresAt: now.Add(ttl), CreatedAt: now,
	}
}

func (t DashboardAuthToken) Active(now time.Time) bool {
	return t.ConsumedAt == nil && now.Before(t.ExpiresAt)
}
