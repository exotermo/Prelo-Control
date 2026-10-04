package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type ProjectRepository struct{ pool *pgxpool.Pool }

func NewProjectRepository(pool *pgxpool.Pool) *ProjectRepository {
	return &ProjectRepository{pool: pool}
}

func (r *ProjectRepository) Insert(ctx context.Context, project domain.Project) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO projects (id, name, description, created_at, created_by, project_version, cover_color)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		project.ID.Value, project.Name, project.Description, project.CreatedAt, nullString(project.CreatedBy), project.Version, project.CoverColor)
	return err
}

const projectSelect = `SELECT id, name, description, created_at, created_by, project_version, deleted_at,
	       default_agent_id, instructions, cover_color
	  FROM projects WHERE deleted_at IS NULL`

func (r *ProjectRepository) FindByID(ctx context.Context, id domain.ProjectID) (domain.Project, error) {
	return scanProject(r.pool.QueryRow(ctx, projectSelect+" AND id = $1", id.Value))
}

func (r *ProjectRepository) List(ctx context.Context) ([]domain.Project, error) {
	rows, err := r.pool.Query(ctx, projectSelect+" ORDER BY created_at DESC")
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

func (r *ProjectRepository) SoftDelete(ctx context.Context, id domain.ProjectID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE projects SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrProjectNotFound
	}
	return nil
}

// Update writes the Fase PA settings, version-checked like every other aggregate.
func (r *ProjectRepository) Update(ctx context.Context, p domain.Project) (domain.Project, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE projects
		   SET name = $2, description = $3, default_agent_id = $4, instructions = $5, cover_color = $6,
		       project_version = project_version + 1
		 WHERE id = $1 AND project_version = $7 AND deleted_at IS NULL
		 RETURNING project_version`,
		p.ID.Value, p.Name, p.Description, p.DefaultAgentID, p.Instructions, p.CoverColor, p.Version)
	if err := row.Scan(&p.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Project{}, application.ErrOptimisticLock
		}
		return domain.Project{}, err
	}
	return p, nil
}

func scanProject(row pgx.Row) (domain.Project, error) {
	var p domain.Project
	var createdBy *string
	if err := row.Scan(&p.ID.Value, &p.Name, &p.Description, &p.CreatedAt, &createdBy, &p.Version, &p.DeletedAt,
		&p.DefaultAgentID, &p.Instructions, &p.CoverColor); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Project{}, application.ErrProjectNotFound
		}
		return domain.Project{}, err
	}
	if createdBy != nil {
		p.CreatedBy = *createdBy
	}
	return p, nil
}
