-- +goose Up
-- PR-1 (contratos G1): the Prelo instance is the workspace, with a stable id generated once.
-- A single row (singleton) today; the column set already fits several workspaces later.
CREATE TABLE prelo_app.workspace (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(120) NOT NULL DEFAULT 'Prelo Control',
    singleton  BOOLEAN NOT NULL DEFAULT true UNIQUE CHECK (singleton),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO prelo_app.workspace DEFAULT VALUES;

-- +goose Down
DROP TABLE IF EXISTS prelo_app.workspace;
