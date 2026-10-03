package domain

import (
	"time"

	"github.com/google/uuid"
)

type DashboardRecoveryCodeID struct{ Value uuid.UUID }

func NewDashboardRecoveryCodeID() DashboardRecoveryCodeID {
	return DashboardRecoveryCodeID{Value: uuid.New()}
}

func (id DashboardRecoveryCodeID) String() string { return id.Value.String() }

// DashboardRecoveryCode is one of the 8 single-use codes minted when TOTP is first confirmed —
// the fallback if the user loses their authenticator app. Only CodeHash (SHA-256) is stored.
type DashboardRecoveryCode struct {
	ID         DashboardRecoveryCodeID
	UserID     DashboardUserID
	CodeHash   []byte
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

func NewDashboardRecoveryCode(userID DashboardUserID, codeHash []byte) DashboardRecoveryCode {
	return DashboardRecoveryCode{ID: NewDashboardRecoveryCodeID(), UserID: userID, CodeHash: codeHash, CreatedAt: time.Now().UTC()}
}
