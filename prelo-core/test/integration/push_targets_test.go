package integration

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
)

// G11: pushes follow the event-stream visibility — ADMIN always, OPERATOR only for own projects,
// no project = everyone — and only active app sessions with a token.
func TestPushTargetsFollowProjectVisibility(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := postgres.NewDashboardUserRepository(pool)
	sessions := postgres.NewMobileSessionRepository(pool)
	projects := postgres.NewProjectRepository(pool)
	members := postgres.NewProjectMemberRepository(pool)
	now := time.Now().UTC()

	newUser := func(email string, role domain.DashboardRole) domain.DashboardUser {
		u, _ := domain.NewDashboardUser(email, role)
		if err := users.Insert(ctx, u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	newSession := func(u domain.DashboardUser, token string) uuid.UUID {
		s, _ := domain.NewMobileSession(u.ID, uuid.New(), "Moto", "android", now)
		if err := sessions.Insert(ctx, s, []byte(uuid.NewString())); err != nil {
			t.Fatal(err)
		}
		if err := sessions.SetPushToken(ctx, s.ID, token, now); err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	admin := newUser("admin@example.com", domain.DashboardRoleAdmin)
	member := newUser("member@example.com", domain.DashboardRoleOperator)
	outsider := newUser("outsider@example.com", domain.DashboardRoleOperator)
	newSession(admin, "tok-admin")
	newSession(member, "tok-member")
	newSession(outsider, "tok-outsider")
	project := mustInsertProject(t, ctx, projects, "Loja")
	if err := members.Add(ctx, domain.ProjectMember{ProjectID: project, UserID: member.ID, AddedAt: now, AddedBy: "tester"}); err != nil {
		t.Fatal(err)
	}

	targets := func(p *uuid.UUID) []string {
		got, err := sessions.PushTargets(ctx, p, now)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(got)
		return got
	}
	if got := targets(&project.Value); len(got) != 2 || got[0] != "tok-admin" || got[1] != "tok-member" {
		t.Fatalf("project targets: %v", got)
	}
	if got := targets(nil); len(got) != 3 {
		t.Fatalf("unassigned work reaches everyone: %v", got)
	}

	// Same phone logs in again: the token moves to the new session, never duplicated.
	again := newSession(member, "tok-member")
	if got := targets(&project.Value); len(got) != 2 {
		t.Fatalf("token must not be duplicated: %v", got)
	}
	// Logout/revocation stops pushes; FCM saying "unregistered" drops the token.
	if err := sessions.Revoke(ctx, again, "logout"); err != nil {
		t.Fatal(err)
	}
	if err := sessions.ForgetPushToken(ctx, "tok-admin"); err != nil {
		t.Fatal(err)
	}
	if got := targets(&project.Value); len(got) != 0 {
		t.Fatalf("revoked/forgotten sessions must not receive pushes: %v", got)
	}
}
