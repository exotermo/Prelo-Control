-- +goose Up
-- Executor identities are separate from SSH servers and project integration API keys.
-- Each credential is bound to exactly one project and never grants a dashboard scope.
CREATE TABLE executor_workers (
    id UUID PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    project_id UUID NOT NULL REFERENCES projects(id),
    image_digest VARCHAR(71) NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ
);
CREATE INDEX executor_workers_project_idx ON executor_workers(project_id) WHERE enabled;

-- +goose Down
DROP TABLE IF EXISTS executor_workers;
