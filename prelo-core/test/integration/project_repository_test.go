package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
)

func TestProjectRepository_InsertFindListSoftDelete(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewProjectRepository(pool)
	ctx := context.Background()

	desc := "client SaaS"
	project, err := domain.NewProject("Client SaaS", &desc, "tester")
	if err != nil {
		t.Fatalf("NewProject failed: %v", err)
	}
	if err := repo.Insert(ctx, project); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	found, err := repo.FindByID(ctx, project.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if found.Name != "Client SaaS" || found.Description == nil || *found.Description != desc {
		t.Fatalf("unexpected project: %+v", found)
	}

	all, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 project, got %d", len(all))
	}

	if err := repo.SoftDelete(ctx, project.ID); err != nil {
		t.Fatalf("soft delete failed: %v", err)
	}
	if _, err := repo.FindByID(ctx, project.ID); !errors.Is(err, application.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound after soft delete, got %v", err)
	}
	all, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after delete failed: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected 0 projects after soft delete, got %d", len(all))
	}
}

func TestProjectMemberRepository_AddRemoveIsMemberListProjectsForUser(t *testing.T) {
	pool := newTestPool(t)
	projects := postgres.NewProjectRepository(pool)
	users := postgres.NewDashboardUserRepository(pool)
	members := postgres.NewProjectMemberRepository(pool)
	ctx := context.Background()

	project, _ := domain.NewProject("Work Project", nil, "tester")
	if err := projects.Insert(ctx, project); err != nil {
		t.Fatalf("project insert failed: %v", err)
	}

	user, err := domain.NewDashboardUser("operator@example.com", domain.DashboardRoleOperator)
	if err != nil {
		t.Fatalf("NewDashboardUser failed: %v", err)
	}
	if err := users.Insert(ctx, user); err != nil {
		t.Fatalf("user insert failed: %v", err)
	}

	isMember, err := members.IsMember(ctx, project.ID, user.ID)
	if err != nil {
		t.Fatalf("IsMember failed: %v", err)
	}
	if isMember {
		t.Fatal("expected not a member before Add")
	}

	if err := members.Add(ctx, domain.NewProjectMember(project.ID, user.ID, "admin")); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	isMember, err = members.IsMember(ctx, project.ID, user.ID)
	if err != nil {
		t.Fatalf("IsMember failed: %v", err)
	}
	if !isMember {
		t.Fatal("expected a member after Add")
	}

	projectsForUser, err := members.ListProjectsForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListProjectsForUser failed: %v", err)
	}
	if len(projectsForUser) != 1 || projectsForUser[0].ID != project.ID {
		t.Fatalf("unexpected projects for user: %+v", projectsForUser)
	}

	if err := members.Remove(ctx, project.ID, user.ID); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	isMember, err = members.IsMember(ctx, project.ID, user.ID)
	if err != nil {
		t.Fatalf("IsMember failed: %v", err)
	}
	if isMember {
		t.Fatal("expected not a member after Remove")
	}
}
