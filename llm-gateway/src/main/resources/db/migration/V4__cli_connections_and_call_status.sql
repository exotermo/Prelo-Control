-- Fase X: subscription CLI connections (Claude Code / Codex via cli-runner, no API key) and the
-- outcome of the last real call, so the dashboard shows a broken connection before anyone asks.
ALTER TABLE provider_connections DROP CONSTRAINT IF EXISTS provider_connections_provider_check;
ALTER TABLE provider_connections ADD CONSTRAINT provider_connections_provider_check
    CHECK (provider IN ('anthropic', 'openai', 'openai_compatible', 'claude_cli', 'codex_cli'));
ALTER TABLE provider_connections
    ADD COLUMN last_call_at TIMESTAMPTZ,
    ADD COLUMN last_call_ok BOOLEAN,
    ADD COLUMN last_call_error VARCHAR(300);
