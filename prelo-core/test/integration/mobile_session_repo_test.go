package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
)

// PR-2: refresh rotation is atomic; a spent token presented again kills the whole session.
func TestMobileSessionRotationAndReuseDetection(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := postgres.NewDashboardUserRepository(pool)
	repo := postgres.NewMobileSessionRepository(pool)
	user, _ := domain.NewDashboardUser("app@example.com", domain.DashboardRoleOperator)
	if err := users.Insert(ctx, user); err != nil {
		t.Fatal(err)
	}
	device := uuid.New()
	now := time.Now().UTC()
	session, _ := domain.NewMobileSession(user.ID, device, "Moto", "android", now)
	if err := repo.Insert(ctx, session, []byte("t1")); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Rotate(ctx, []byte("t1"), []byte("t2"), uuid.New(), now); !errors.Is(err, application.ErrMobileRefreshInvalid) {
		t.Fatalf("another device must not use this token, got %v", err)
	}
	later := now.Add(time.Hour)
	rotated, err := repo.Rotate(ctx, []byte("t1"), []byte("t2"), device, later)
	if err != nil || !rotated.LastUsedAt.Equal(later) {
		t.Fatalf("rotation failed: %+v %v", rotated, err)
	}
	if _, err := repo.Rotate(ctx, []byte("t2"), []byte("t3"), device, later); err != nil {
		t.Fatalf("the new token must work: %v", err)
	}
	// t1 again: someone kept a copy → the session dies, even for the legitimate t3.
	if _, err := repo.Rotate(ctx, []byte("t1"), []byte("x"), device, later); !errors.Is(err, application.ErrMobileRefreshReused) {
		t.Fatalf("reuse must be detected, got %v", err)
	}
	if _, err := repo.Rotate(ctx, []byte("t3"), []byte("t4"), device, later); !errors.Is(err, application.ErrMobileRefreshInvalid) {
		t.Fatalf("after reuse the whole family is dead, got %v", err)
	}
	stored, _ := repo.FindByID(ctx, session.ID)
	if stored.RevokedAt == nil || stored.RevokedReason == nil || *stored.RevokedReason != "refresh_reused" {
		t.Fatalf("session should be revoked for reuse: %+v", stored)
	}

	second, _ := domain.NewMobileSession(user.ID, uuid.New(), "Tablet", "android", now)
	_ = repo.Insert(ctx, second, []byte("s1"))
	active, _ := repo.ListActiveByUser(ctx, user.ID, now)
	if len(active) != 1 || active[0].ID != second.ID {
		t.Fatalf("only the live session should be listed: %+v", active)
	}
	if n, err := repo.RevokeAllForUser(ctx, user.ID, "role_changed"); err != nil || n != 1 {
		t.Fatalf("revoke all: %d %v", n, err)
	}
	if byToken, err := repo.FindByTokenHash(ctx, []byte("s1")); err != nil || byToken.RevokedAt == nil {
		t.Fatalf("lookup by token: %+v %v", byToken, err)
	}
}
