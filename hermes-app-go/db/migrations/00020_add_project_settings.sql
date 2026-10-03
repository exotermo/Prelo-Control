-- +goose Up
-- Fase PA: per-project settings edited from the cover's Configurações tab.
ALTER TABLE hermes_app.projects
    ADD COLUMN default_agent_id VARCHAR(100),
    ADD COLUMN instructions TEXT,
    ADD COLUMN cover_color VARCHAR(16) NOT NULL DEFAULT 'ink';

-- +goose Down
ALTER TABLE hermes_app.projects
    DROP COLUMN IF EXISTS default_agent_id,
    DROP COLUMN IF EXISTS instructions,
    DROP COLUMN IF EXISTS cover_color;
