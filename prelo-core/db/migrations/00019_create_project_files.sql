-- +goose Up
-- Fase PA: project files. The bytes live sealed on a volume (filestore: per-file HKDF key,
-- chunked AES-256-GCM); this table only holds metadata and the non-secret parameters needed to
-- read them back (salt, nonce prefix, chunk size).
CREATE TABLE prelo_app.project_files (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES prelo_app.projects(id),
    name VARCHAR(255) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('pdf', 'image', 'text', 'other')),
    size_bytes BIGINT NOT NULL,
    sha256 BYTEA NOT NULL,
    salt BYTEA NOT NULL,
    nonce_prefix BYTEA NOT NULL,
    chunk_size INTEGER NOT NULL,
    uploaded_by VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX project_files_project_idx ON prelo_app.project_files (project_id, created_at DESC) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS prelo_app.project_files;
