package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// MobileSession (PR-2, contratos G2) is one signed-in device of the Work Control app. The server
// is the authority: the app only holds an opaque, rotating refresh token for it.
type MobileSession struct {
	ID                uuid.UUID
	UserID            DashboardUserID
	DeviceID          uuid.UUID
	DeviceName        string
	Platform          string
	CreatedAt         time.Time
	LastUsedAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	LastTotpAt        *time.Time
	RevokedAt         *time.Time
	RevokedReason     *string
}

const (
	MobileSessionIdleTTL     = 30 * 24 * time.Hour
	MobileSessionAbsoluteTTL = 90 * 24 * time.Hour
	// StepUpWindow: a HIGH-risk approval from the app needs a TOTP checked this recently.
	StepUpWindow = 5 * time.Minute
)

func NewMobileSession(userID DashboardUserID, deviceID uuid.UUID, deviceName, platform string, now time.Time) (MobileSession, error) {
	name := strings.TrimSpace(deviceName)
	if name == "" {
		name = "Aparelho"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	if platform != "android" && platform != "ios" {
		return MobileSession{}, &ValidationError{Message: "platform must be android or ios"}
	}
	if deviceID == uuid.Nil {
		return MobileSession{}, &ValidationError{Message: "deviceId is required"}
	}
	totp := now
	return MobileSession{
		ID: uuid.New(), UserID: userID, DeviceID: deviceID, DeviceName: name, Platform: platform,
		CreatedAt: now, LastUsedAt: now, IdleExpiresAt: now.Add(MobileSessionIdleTTL),
		AbsoluteExpiresAt: now.Add(MobileSessionAbsoluteTTL), LastTotpAt: &totp,
	}, nil
}

// Active: not revoked and inside both the sliding and the absolute window.
func (s MobileSession) Active(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.IdleExpiresAt) && now.Before(s.AbsoluteExpiresAt)
}

// RecentTotp: a TOTP was checked within the step-up window (login counts).
func (s MobileSession) RecentTotp(now time.Time) bool {
	return s.LastTotpAt != nil && now.Sub(*s.LastTotpAt) <= StepUpWindow
}

// NextIdleExpiry slides the idle window, never past the absolute limit.
func (s MobileSession) NextIdleExpiry(now time.Time) time.Time {
	next := now.Add(MobileSessionIdleTTL)
	if next.After(s.AbsoluteExpiresAt) {
		return s.AbsoluteExpiresAt
	}
	return next
}
