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

type ServerRepository struct{ pool *pgxpool.Pool }

func NewServerRepository(pool *pgxpool.Pool) *ServerRepository {
	return &ServerRepository{pool: pool}
}

func (r *ServerRepository) Insert(ctx context.Context, server domain.Server) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO servers (id, project_id, name, host, ssh_port, ssh_user, credential_kind, encrypted_private_key,
		                      host_key_fingerprint, host_key_captured_at, last_status, created_at, created_by, server_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		server.ID.Value, projectIDValue(server.ProjectID), server.Name, server.Host, server.SSHPort, server.SSHUser, string(server.CredentialKind),
		server.EncryptedPrivateKey, server.HostKeyFingerprint, server.HostKeyCapturedAt, string(server.LastStatus),
		server.CreatedAt, nullString(server.CreatedBy), server.Version)
	return err
}

const serverSelect = `SELECT id, project_id, name, host, ssh_port, ssh_user, credential_kind, encrypted_private_key,
	       host_key_fingerprint, host_key_captured_at, last_status, last_checked_at, last_error,
	       created_at, created_by, server_version
	  FROM servers WHERE deleted_at IS NULL`

func (r *ServerRepository) FindByID(ctx context.Context, id domain.ServerID) (domain.Server, error) {
	return scanServer(r.pool.QueryRow(ctx, serverSelect+" AND id = $1", id.Value))
}

// List excludes soft-deleted servers, newest first, across every project — the ADMIN-wide view.
// The dashboard's ServerHandler.List uses ListByProject instead (Fase W).
func (r *ServerRepository) List(ctx context.Context) ([]domain.Server, error) {
	rows, err := r.pool.Query(ctx, serverSelect+" ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var servers []domain.Server
	for rows.Next() {
		server, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

// ListByProject backs the Fase W project-scoped Servidores page: projectID nil means the
// "unassigned" bucket (project_id IS NULL), matching every pre-Fase-W server.
func (r *ServerRepository) ListByProject(ctx context.Context, projectID *uuid.UUID) ([]domain.Server, error) {
	var rows pgx.Rows
	var err error
	if projectID == nil {
		rows, err = r.pool.Query(ctx, serverSelect+" AND project_id IS NULL ORDER BY created_at DESC")
	} else {
		rows, err = r.pool.Query(ctx, serverSelect+" AND project_id = $1 ORDER BY created_at DESC", *projectID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var servers []domain.Server
	for rows.Next() {
		server, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

func (r *ServerRepository) Update(ctx context.Context, server domain.Server) (domain.Server, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE servers
		   SET name = $2, host = $3, ssh_port = $4, ssh_user = $5,
		       encrypted_private_key = $6, host_key_fingerprint = $7, host_key_captured_at = $8,
		       last_status = $9, last_checked_at = $10, last_error = $11, server_version = server_version + 1
		 WHERE id = $1 AND server_version = $12 AND deleted_at IS NULL
		 RETURNING server_version`,
		server.ID.Value, server.Name, server.Host, server.SSHPort, server.SSHUser,
		server.EncryptedPrivateKey, server.HostKeyFingerprint, server.HostKeyCapturedAt,
		string(server.LastStatus), server.LastCheckedAt, server.LastError, server.Version)

	var newVersion int64
	if err := row.Scan(&newVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Server{}, application.ErrOptimisticLock
		}
		return domain.Server{}, err
	}
	server.Version = newVersion
	return server, nil
}

func (r *ServerRepository) SoftDelete(ctx context.Context, id domain.ServerID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE servers SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrServerNotFound
	}
	return nil
}

func scanServer(row pgx.Row) (domain.Server, error) {
	var s domain.Server
	var credentialKind, lastStatus string
	var createdBy *string
	var projectID *uuid.UUID
	if err := row.Scan(&s.ID.Value, &projectID, &s.Name, &s.Host, &s.SSHPort, &s.SSHUser, &credentialKind, &s.EncryptedPrivateKey,
		&s.HostKeyFingerprint, &s.HostKeyCapturedAt, &lastStatus, &s.LastCheckedAt, &s.LastError,
		&s.CreatedAt, &createdBy, &s.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Server{}, application.ErrServerNotFound
		}
		return domain.Server{}, err
	}
	s.CredentialKind = domain.ServerCredentialKind(credentialKind)
	s.LastStatus = domain.ServerStatus(lastStatus)
	if createdBy != nil {
		s.CreatedBy = *createdBy
	}
	if projectID != nil {
		id := domain.ProjectID{Value: *projectID}
		s.ProjectID = &id
	}
	return s, nil
}

// projectIDValue converts a domain.ProjectID pointer to the *uuid.UUID a query parameter needs —
// nil stays nil (project_id IS NULL), shared by every ...ByProject-aware Insert in this package.
func projectIDValue(id *domain.ProjectID) *uuid.UUID {
	if id == nil {
		return nil
	}
	return &id.Value
}
