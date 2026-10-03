-- +goose Up
-- Fase G1: human login (password + mandatory TOTP) for hermes-dashboard. Single-instance, no
-- RBAC roles — every activated user has full access, unlike messaging-core's multi-tenant RBAC.
CREATE TABLE hermes_app.dashboard_users (
    id                     UUID PRIMARY KEY,
    email                  TEXT NOT NULL UNIQUE,
    password_hash          TEXT,
    totp_secret_encrypted  BYTEA,
    totp_enabled           BOOLEAN NOT NULL DEFAULT false,
    failed_logins          INT NOT NULL DEFAULT 0,
    locked_until           TIMESTAMPTZ,
    activated_at           TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One table for every kind of one-time token (invite/activation, password reset, TOTP
-- challenge, refresh) — same shape as messaging-core's dashboard_auth_tokens. The raw token is
-- never stored, only its SHA-256 hash; a token is single-use, enforced by the guarded UPDATE
-- that sets consumed_at (see DashboardAuthTokenRepository.Consume).
CREATE TABLE hermes_app.dashboard_auth_tokens (
    id           UUID PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES hermes_app.dashboard_users(id),
    token_hash   BYTEA NOT NULL,
    purpose      TEXT NOT NULL CHECK (purpose IN ('INVITE', 'RESET', 'CHALLENGE', 'REFRESH')),
    expires_at   TIMESTAMPTZ NOT NULL,
    consumed_at  TIMESTAMPTZ,
    attempts     INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX dashboard_auth_tokens_lookup_idx ON hermes_app.dashboard_auth_tokens (purpose, token_hash) WHERE consumed_at IS NULL;
CREATE INDEX dashboard_auth_tokens_user_idx ON hermes_app.dashboard_auth_tokens (user_id);

CREATE TABLE hermes_app.dashboard_recovery_codes (
    id           UUID PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES hermes_app.dashboard_users(id),
    code_hash    BYTEA NOT NULL,
    consumed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX dashboard_recovery_codes_user_idx ON hermes_app.dashboard_recovery_codes (user_id) WHERE consumed_at IS NULL;

-- Login rate limiting, keyed by a caller-chosen string (typically a hash of email+IP) — a
-- sliding window counter, reset once window_start is older than the configured window.
CREATE TABLE hermes_app.dashboard_auth_rate_limits (
    key_hash     BYTEA PRIMARY KEY,
    attempts     INT NOT NULL DEFAULT 0,
    window_start TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS hermes_app.dashboard_auth_rate_limits;
DROP TABLE IF EXISTS hermes_app.dashboard_recovery_codes;
DROP TABLE IF EXISTS hermes_app.dashboard_auth_tokens;
DROP TABLE IF EXISTS hermes_app.dashboard_users;
