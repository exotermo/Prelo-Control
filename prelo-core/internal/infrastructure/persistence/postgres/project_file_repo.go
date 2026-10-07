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

type ProjectFileRepository struct{ pool *pgxpool.Pool }

func NewProjectFileRepository(pool *pgxpool.Pool) *ProjectFileRepository {
	return &ProjectFileRepository{pool: pool}
}

const projectFileColumns = `id, project_id, name, content_type, kind, size_bytes, sha256, salt, nonce_prefix, chunk_size, uploaded_by, created_at, deleted_at, relative_path, origin_task_id, origin_execution_id, executor_request_id`

func (r *ProjectFileRepository) Insert(ctx context.Context, f domain.ProjectFile) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO project_files (id, project_id, name, content_type, kind, size_bytes, sha256, salt, nonce_prefix, chunk_size, uploaded_by, relative_path, origin_task_id, origin_execution_id, executor_request_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		f.ID, f.ProjectID.Value, f.Name, f.ContentType, f.Kind, f.SizeBytes, f.SHA256, f.Salt, f.NoncePrefix, f.ChunkSize, nullString(f.UploadedBy), f.RelativePath, nullableTaskID(f.OriginTaskID), nullableExecutionID(f.OriginExecutionID), f.ExecutorRequestID)
	return err
}
func nullableTaskID(id *domain.TaskID) *uuid.UUID {
	if id == nil {
		return nil
	}
	return &id.Value
}
func nullableExecutionID(id *domain.ExecutionID) *uuid.UUID {
	if id == nil {
		return nil
	}
	return &id.Value
}

func (r *ProjectFileRepository) FindByID(ctx context.Context, id uuid.UUID) (domain.ProjectFile, error) {
	return scanProjectFile(r.pool.QueryRow(ctx, `SELECT `+projectFileColumns+` FROM project_files WHERE id = $1`, id))
}

func (r *ProjectFileRepository) ListByProject(ctx context.Context, projectID domain.ProjectID) ([]domain.ProjectFile, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+projectFileColumns+` FROM project_files
		WHERE project_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`, projectID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []domain.ProjectFile
	for rows.Next() {
		f, err := scanProjectFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (r *ProjectFileRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE project_files SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return err
}

func scanProjectFile(row pgx.Row) (domain.ProjectFile, error) {
	var f domain.ProjectFile
	var uploadedBy *string
	var originTaskID, originExecutionID *uuid.UUID
	if err := row.Scan(&f.ID, &f.ProjectID.Value, &f.Name, &f.ContentType, &f.Kind, &f.SizeBytes, &f.SHA256, &f.Salt,
		&f.NoncePrefix, &f.ChunkSize, &uploadedBy, &f.CreatedAt, &f.DeletedAt, &f.RelativePath, &originTaskID, &originExecutionID, &f.ExecutorRequestID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ProjectFile{}, application.ErrProjectFileNotFound
		}
		return domain.ProjectFile{}, err
	}
	if uploadedBy != nil {
		f.UploadedBy = *uploadedBy
	}
	if originTaskID != nil {
		f.OriginTaskID = &domain.TaskID{Value: *originTaskID}
	}
	if originExecutionID != nil {
		f.OriginExecutionID = &domain.ExecutionID{Value: *originExecutionID}
	}
	return f, nil
}
