-- +goose Up
-- PR-2 (contratos G2/G9): app sessions, one per device. The refresh token is opaque and rotates on
-- every use; every issued token is kept (hash only) so presenting an already-used one is detected
-- as theft and revokes the whole session ("family").
CREATE TABLE prelo_app.mobile_sessions (
    id                  UUID PRIMARY KEY,
    user_id             UUID NOT NULL REFERENCES prelo_app.dashboard_users(id),
    device_id           UUID NOT NULL,
    device_name         VARCHAR(80) NOT NULL,
    platform            VARCHAR(16) NOT NULL CHECK (platform IN ('android', 'ios')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    idle_expires_at     TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    last_totp_at        TIMESTAMPTZ,
    revoked_at          TIMESTAMPTZ,
    revoked_reason      VARCHAR(40)
);
CREATE INDEX mobile_sessions_user_idx ON prelo_app.mobile_sessions (user_id, last_used_at DESC) WHERE revoked_at IS NULL;

CREATE TABLE prelo_app.mobile_session_tokens (
    token_hash BYTEA PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES prelo_app.mobile_sessions(id) ON DELETE CASCADE,
    issued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    used_at    TIMESTAMPTZ
);
CREATE INDEX mobile_session_tokens_session_idx ON prelo_app.mobile_session_tokens (session_id);

-- +goose Down
DROP TABLE IF EXISTS prelo_app.mobile_session_tokens;
DROP TABLE IF EXISTS prelo_app.mobile_sessions;
