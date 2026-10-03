-- +goose Up
-- Fase I: per-project integrations — API keys external apps use to call Hermes, outbound
-- webhooks, and the durable outbox their deliveries drain from.
CREATE TABLE hermes_app.api_keys (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES hermes_app.projects(id),
    name VARCHAR(120) NOT NULL,
    display_prefix VARCHAR(32) NOT NULL,
    key_hash BYTEA NOT NULL,
    scopes TEXT[] NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by VARCHAR(255),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX api_keys_key_hash_idx ON hermes_app.api_keys (key_hash);
CREATE INDEX api_keys_project_idx ON hermes_app.api_keys (project_id);

CREATE TABLE hermes_app.webhooks (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES hermes_app.projects(id),
    name VARCHAR(120) NOT NULL,
    url VARCHAR(2000) NOT NULL,
    events TEXT[] NOT NULL,
    encrypted_secret BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by VARCHAR(255),
    disabled_at TIMESTAMPTZ
);
CREATE INDEX webhooks_project_idx ON hermes_app.webhooks (project_id) WHERE disabled_at IS NULL;

CREATE TABLE hermes_app.webhook_deliveries (
    id UUID PRIMARY KEY,
    webhook_id UUID NOT NULL REFERENCES hermes_app.webhooks(id),
    event VARCHAR(64) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    attempt INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    last_status_code INTEGER,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ
);
CREATE INDEX webhook_deliveries_due_idx ON hermes_app.webhook_deliveries (available_at)
    WHERE status IN ('PENDING', 'RETRY', 'SENDING');
CREATE INDEX webhook_deliveries_webhook_idx ON hermes_app.webhook_deliveries (webhook_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS hermes_app.webhook_deliveries;
DROP TABLE IF EXISTS hermes_app.webhooks;
DROP TABLE IF EXISTS hermes_app.api_keys;
