-- +goose Up
-- Fase S1: registered remote servers prelo-core SSHes into directly (no tunnel yet) for health
-- checks. Soft delete (deleted_at) keeps a removed server's audit trail instead of losing it.
CREATE TABLE prelo_app.servers (
    id UUID PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    host VARCHAR(255) NOT NULL,
    ssh_port INTEGER NOT NULL DEFAULT 22,
    ssh_user VARCHAR(120) NOT NULL,
    credential_kind VARCHAR(30) NOT NULL DEFAULT 'SSH_PRIVATE_KEY',
    encrypted_private_key BYTEA NOT NULL,
    host_key_fingerprint VARCHAR(100) NOT NULL,
    host_key_captured_at TIMESTAMPTZ NOT NULL,
    last_status VARCHAR(20) NOT NULL DEFAULT 'UNKNOWN',
    last_checked_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by VARCHAR(255),
    server_version BIGINT NOT NULL DEFAULT 0,
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX servers_host_port_active_idx
    ON prelo_app.servers (host, ssh_port) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS prelo_app.servers;
