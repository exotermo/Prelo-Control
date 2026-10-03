-- +goose Up
-- Fase W: Projects are isolated work environments (e.g. a client's SaaS vs. a work project),
-- each scoping its own Tasks and Servers. Soft delete keeps a removed project's audit trail.
CREATE TABLE hermes_app.projects (
    id UUID PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by VARCHAR(255),
    project_version BIGINT NOT NULL DEFAULT 0,
    deleted_at TIMESTAMPTZ
);

-- Binary membership (no role within the project) — the actual access boundary: an OPERATOR
-- dashboard session may only select a project it has a row here for (see
-- JWTAuthMiddleware.resolveProject); ADMIN (projects:manage) bypasses this check entirely.
CREATE TABLE hermes_app.project_members (
    project_id UUID NOT NULL REFERENCES hermes_app.projects(id),
    dashboard_user_id UUID NOT NULL REFERENCES hermes_app.dashboard_users(id),
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by VARCHAR(255),
    PRIMARY KEY (project_id, dashboard_user_id)
);

-- NULL means the "unassigned" bucket — every row that existed before this migration, or created
-- with no project context selected, stays visible there exactly as before (no data migration).
ALTER TABLE hermes_app.tasks ADD COLUMN project_id UUID REFERENCES hermes_app.projects(id);
ALTER TABLE hermes_app.servers ADD COLUMN project_id UUID REFERENCES hermes_app.projects(id);

CREATE INDEX tasks_project_id_idx ON hermes_app.tasks (project_id);
CREATE INDEX servers_project_id_idx ON hermes_app.servers (project_id);

-- +goose Down
ALTER TABLE hermes_app.servers DROP COLUMN IF EXISTS project_id;
ALTER TABLE hermes_app.tasks DROP COLUMN IF EXISTS project_id;
DROP TABLE IF EXISTS hermes_app.project_members;
DROP TABLE IF EXISTS hermes_app.projects;
