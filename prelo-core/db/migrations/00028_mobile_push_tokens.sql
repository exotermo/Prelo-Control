-- +goose Up
-- G11 (push no celular): the FCM registration token of each app session. One per session (device);
-- revoking or expiring the session stops its pushes, since targets only come from active sessions.
-- A token is unique: when Android hands the same token to a new login, the old session loses it.
ALTER TABLE prelo_app.mobile_sessions
    ADD COLUMN push_token            TEXT,
    ADD COLUMN push_token_updated_at TIMESTAMPTZ;
CREATE UNIQUE INDEX mobile_sessions_push_token_idx ON prelo_app.mobile_sessions (push_token) WHERE push_token IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS prelo_app.mobile_sessions_push_token_idx;
ALTER TABLE prelo_app.mobile_sessions DROP COLUMN IF EXISTS push_token_updated_at, DROP COLUMN IF EXISTS push_token;
