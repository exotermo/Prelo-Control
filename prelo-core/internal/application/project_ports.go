package application

import (
	"context"
	"errors"

	"github.com/exotermo/prelo-core/internal/domain"
)

var ErrProjectNotFound = errors.New("project not found")

// ErrNotProjectMember is returned (and mapped to 403, not 404 — see auth.go) when a caller
// without projects:manage asks to act on a project they are not a member of. It is deliberately
// the same outward shape whether the project doesn't exist or the caller just isn't a member —
// ProjectAuthMiddleware never lets a non-member distinguish the two.
var ErrNotProjectMember = errors.New("not a member of this project")

// ProjectRepository persists Projects (Fase W). Update/SoftDelete are version-checked, same
// convention as ServerRepository/TaskRepository.
type ProjectRepository interface {
	Insert(ctx context.Context, project domain.Project) error
	FindByID(ctx context.Context, id domain.ProjectID) (domain.Project, error)
	// List excludes soft-deleted projects, newest first — the ADMIN-wide view (an OPERATOR's
	// view instead comes from ProjectMemberRepository.ListProjectsForUser).
	List(ctx context.Context) ([]domain.Project, error)
	SoftDelete(ctx context.Context, id domain.ProjectID) error
	Update(ctx context.Context, project domain.Project) (domain.Project, error)
}

// ProjectMemberRepository is the actual access boundary for Fase W: IsMember is what
// ProjectAuthMiddleware calls on every project-scoped request from a caller without
// projects:manage, and ListProjectsForUser is what builds an OPERATOR's project card grid.
type ProjectMemberRepository interface {
	Add(ctx context.Context, member domain.ProjectMember) error
	Remove(ctx context.Context, projectID domain.ProjectID, userID domain.DashboardUserID) error
	ListMembers(ctx context.Context, projectID domain.ProjectID) ([]domain.ProjectMember, error)
	ListProjectsForUser(ctx context.Context, userID domain.DashboardUserID) ([]domain.Project, error)
	IsMember(ctx context.Context, projectID domain.ProjectID, userID domain.DashboardUserID) (bool, error)
}
