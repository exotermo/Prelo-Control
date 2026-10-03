package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

type ProjectMemberRepository struct{ pool *pgxpool.Pool }

func NewProjectMemberRepository(pool *pgxpool.Pool) *ProjectMemberRepository {
	return &ProjectMemberRepository{pool: pool}
}

func (r *ProjectMemberRepository) Add(ctx context.Context, member domain.ProjectMember) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO project_members (project_id, dashboard_user_id, added_at, added_by)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (project_id, dashboard_user_id) DO NOTHING`,
		member.ProjectID.Value, member.UserID.Value, member.AddedAt, nullString(member.AddedBy))
	return err
}

func (r *ProjectMemberRepository) Remove(ctx context.Context, projectID domain.ProjectID, userID domain.DashboardUserID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM project_members WHERE project_id = $1 AND dashboard_user_id = $2`, projectID.Value, userID.Value)
	return err
}

func (r *ProjectMemberRepository) ListMembers(ctx context.Context, projectID domain.ProjectID) ([]domain.ProjectMember, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT project_id, dashboard_user_id, added_at, added_by
		  FROM project_members WHERE project_id = $1 ORDER BY added_at`, projectID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []domain.ProjectMember
	for rows.Next() {
		member, err := scanProjectMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

// ListProjectsForUser builds an OPERATOR's project card grid — every non-deleted Project this
// user has a project_members row for, newest first.
func (r *ProjectMemberRepository) ListProjectsForUser(ctx context.Context, userID domain.DashboardUserID) ([]domain.Project, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.id, p.name, p.description, p.created_at, p.created_by, p.project_version, p.deleted_at,
		       p.default_agent_id, p.instructions, p.cover_color
		  FROM projects p
		  JOIN project_members m ON m.project_id = p.id
		 WHERE m.dashboard_user_id = $1 AND p.deleted_at IS NULL
		 ORDER BY p.created_at DESC`, userID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []domain.Project
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

// IsMember is the actual access-boundary check JWTAuthMiddleware.resolveProject calls for any
// caller without projects:manage.
func (r *ProjectMemberRepository) IsMember(ctx context.Context, projectID domain.ProjectID, userID domain.DashboardUserID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project_members WHERE project_id = $1 AND dashboard_user_id = $2)`,
		projectID.Value, userID.Value).Scan(&exists)
	return exists, err
}

func scanProjectMember(row pgx.Row) (domain.ProjectMember, error) {
	var m domain.ProjectMember
	var addedBy *string
	if err := row.Scan(&m.ProjectID.Value, &m.UserID.Value, &m.AddedAt, &addedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ProjectMember{}, nil
		}
		return domain.ProjectMember{}, err
	}
	if addedBy != nil {
		m.AddedBy = *addedBy
	}
	return m, nil
}
