-- Fase M: provider connections managed from hermes-dashboard. One INSTANCE-wide default plus an
-- optional per-PROJECT override. The API key is never stored in clear: encrypted_key holds
-- nonce || AES-256-GCM ciphertext under a per-record data key derived (HKDF-SHA256) from the
-- master key (a Docker secret, outside this database) and key_salt (32 random bytes per write).
CREATE TABLE provider_connections (
    id UUID PRIMARY KEY,
    scope VARCHAR(16) NOT NULL CHECK (scope IN ('INSTANCE', 'PROJECT')),
    project_id UUID,
    provider VARCHAR(32) NOT NULL CHECK (provider IN ('anthropic', 'openai', 'openai_compatible')),
    base_url VARCHAR(500) NOT NULL,
    model VARCHAR(200) NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    key_salt BYTEA,
    encrypted_key BYTEA,
    key_last4 VARCHAR(4),
    last_test_at TIMESTAMPTZ,
    last_test_ok BOOLEAN,
    last_test_latency_ms BIGINT,
    last_test_error VARCHAR(300),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL DEFAULT 0,
    CHECK ((scope = 'INSTANCE' AND project_id IS NULL) OR (scope = 'PROJECT' AND project_id IS NOT NULL)),
    CHECK ((encrypted_key IS NULL) = (key_salt IS NULL))
);
CREATE UNIQUE INDEX provider_connections_instance_idx ON provider_connections (scope) WHERE scope = 'INSTANCE';
CREATE UNIQUE INDEX provider_connections_project_idx ON provider_connections (project_id) WHERE scope = 'PROJECT';

-- Usage per project for the dashboard's "uso neste projeto" chart.
ALTER TABLE gateway_audit_events
    ADD COLUMN project_id UUID,
    ADD COLUMN input_tokens INT,
    ADD COLUMN output_tokens INT;
CREATE INDEX gateway_audit_events_usage_idx ON gateway_audit_events (project_id, created_at) WHERE event_type = 'LLM_RESPONSE';
