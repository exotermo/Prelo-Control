package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type ProjectToolSettings struct{ pool *pgxpool.Pool }

func NewProjectToolSettings(pool *pgxpool.Pool) *ProjectToolSettings {
	return &ProjectToolSettings{pool: pool}
}

// Get returns the effective setting. No override preserves previously offered tools.
func (s *ProjectToolSettings) Get(ctx context.Context, projectID domain.ProjectID, toolName string) (application.ToolSetting, error) {
	var setting application.ToolSetting
	err := s.pool.QueryRow(ctx, `SELECT enabled, version FROM project_tool_settings WHERE project_id=$1 AND tool_name=$2`, projectID.Value, toolName).Scan(&setting.Enabled, &setting.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ToolSetting{Enabled: true, Version: 0}, nil
	}
	return setting, err
}

func (s *ProjectToolSettings) Allowed(ctx context.Context, projectID domain.ProjectID, toolName string) (bool, error) {
	setting, err := s.Get(ctx, projectID, toolName)
	return setting.Enabled, err
}

// Set uses optimistic concurrency and records each change in the same transaction.
func (s *ProjectToolSettings) Set(ctx context.Context, projectID domain.ProjectID, toolName string, enabled bool, expectedVersion int64, actor string) (application.ToolSetting, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return application.ToolSetting{}, err
	}
	defer tx.Rollback(ctx)
	var version int64
	if expectedVersion == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO project_tool_settings (project_id, tool_name, enabled, changed_by)
			VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING RETURNING version`, projectID.Value, toolName, enabled, actor).Scan(&version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE project_tool_settings SET enabled=$3, version=version+1, changed_by=$4, changed_at=now()
			WHERE project_id=$1 AND tool_name=$2 AND version=$5 RETURNING version`, projectID.Value, toolName, enabled, actor, expectedVersion).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ToolSetting{}, application.ErrOptimisticLock
	}
	if err != nil {
		return application.ToolSetting{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO project_tool_setting_events (id, project_id, tool_name, enabled, version, changed_by)
		VALUES ($1,$2,$3,$4,$5,$6)`, uuid.New(), projectID.Value, toolName, enabled, version, actor)
	if err != nil {
		return application.ToolSetting{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return application.ToolSetting{}, err
	}
	return application.ToolSetting{Enabled: enabled, Version: version}, nil
}
