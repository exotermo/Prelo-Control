package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMobileSessionWindows(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	s, err := NewMobileSession(DashboardUserID{Value: uuid.New()}, uuid.New(), "  Moto g54  ", "android", now)
	if err != nil || s.DeviceName != "Moto g54" || !s.Active(now) || !s.RecentTotp(now) {
		t.Fatalf("new session: %+v %v", s, err)
	}
	if s.RecentTotp(now.Add(6 * time.Minute)) {
		t.Fatal("TOTP from login must stop counting after 5 minutes")
	}
	if s.Active(now.Add(MobileSessionIdleTTL + time.Second)) {
		t.Fatal("idle session must expire")
	}
	if got := s.NextIdleExpiry(now.Add(80 * 24 * time.Hour)); !got.Equal(s.AbsoluteExpiresAt) {
		t.Fatalf("sliding window must stop at the absolute limit, got %v", got)
	}
	revoked := now
	s.RevokedAt = &revoked
	if s.Active(now) {
		t.Fatal("revoked session is not active")
	}
	for _, bad := range []struct {
		device   uuid.UUID
		platform string
	}{{uuid.Nil, "android"}, {uuid.New(), "windows"}} {
		if _, err := NewMobileSession(DashboardUserID{}, bad.device, "x", bad.platform, now); err == nil {
			t.Errorf("%v/%s should be refused", bad.device, bad.platform)
		}
	}
}
