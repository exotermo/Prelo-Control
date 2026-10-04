-- +goose Up
-- Fase C1: clients (the CRM side of Prelo Control), their contacts, the link from projects and
-- tasks to a client, per-user "recently opened" items, and accent-insensitive trigram search.
CREATE EXTENSION IF NOT EXISTS unaccent WITH SCHEMA prelo_app;
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA prelo_app;

-- unaccent() is only STABLE (it depends on the dictionary search path), so it can't back an
-- index directly; pinning the dictionary makes this wrapper safely IMMUTABLE.
-- +goose StatementBegin
CREATE FUNCTION prelo_app.search_norm(text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
    AS $$ SELECT lower(prelo_app.unaccent('prelo_app.unaccent'::regdictionary, $1)) $$;
-- +goose StatementEnd

CREATE TABLE prelo_app.clients (
    id             UUID PRIMARY KEY,
    name           VARCHAR(200) NOT NULL,
    company        VARCHAR(200),
    status         VARCHAR(16) NOT NULL CHECK (status IN ('LEAD', 'ACTIVE', 'INACTIVE', 'DISCARDED')),
    stage          VARCHAR(16) NOT NULL DEFAULT 'NEW' CHECK (stage IN ('NEW', 'ANALYZED', 'CONTACTED', 'REPLIED', 'QUALIFIED')),
    source         VARCHAR(16) NOT NULL CHECK (source IN ('MANUAL', 'OSM', 'WHATSAPP')),
    niche_id       UUID,
    address        VARCHAR(400),
    city           VARCHAR(120),
    latitude       DOUBLE PRECISION,
    longitude      DOUBLE PRECISION,
    website        VARCHAR(500),
    external_ref   VARCHAR(200),
    notes          TEXT,
    gaps           JSONB NOT NULL DEFAULT '[]',
    briefing       JSONB,
    opted_out_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     VARCHAR(255),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    client_version BIGINT NOT NULL DEFAULT 0,
    deleted_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX clients_external_ref_uk ON prelo_app.clients (external_ref) WHERE external_ref IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX clients_updated_idx ON prelo_app.clients (updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX clients_search_idx ON prelo_app.clients
    USING gin (prelo_app.search_norm(name || ' ' || coalesce(company, '') || ' ' || coalesce(city, '')) prelo_app.gin_trgm_ops);

CREATE TABLE prelo_app.client_contacts (
    id         UUID PRIMARY KEY,
    client_id  UUID NOT NULL REFERENCES prelo_app.clients(id) ON DELETE CASCADE,
    kind       VARCHAR(16) NOT NULL CHECK (kind IN ('PHONE', 'WHATSAPP', 'EMAIL')),
    value      VARCHAR(255) NOT NULL,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- A phone/e-mail belongs to one client at a time — Fase C2 routes WhatsApp by this value.
CREATE UNIQUE INDEX client_contacts_value_uk ON prelo_app.client_contacts (kind, value);
CREATE INDEX client_contacts_client_idx ON prelo_app.client_contacts (client_id);
CREATE INDEX client_contacts_search_idx ON prelo_app.client_contacts USING gin (value prelo_app.gin_trgm_ops);

ALTER TABLE prelo_app.projects ADD COLUMN client_id UUID REFERENCES prelo_app.clients(id);
CREATE INDEX projects_client_idx ON prelo_app.projects (client_id) WHERE client_id IS NOT NULL;
ALTER TABLE prelo_app.tasks ADD COLUMN client_id UUID REFERENCES prelo_app.clients(id);
CREATE INDEX tasks_client_idx ON prelo_app.tasks (client_id, created_at DESC) WHERE client_id IS NOT NULL;

CREATE TABLE prelo_app.user_recent_items (
    user_id   UUID NOT NULL REFERENCES prelo_app.dashboard_users(id),
    kind      VARCHAR(16) NOT NULL CHECK (kind IN ('CLIENT', 'PROJECT', 'TASK')),
    ref_id    UUID NOT NULL,
    viewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, kind, ref_id)
);
CREATE INDEX user_recent_items_user_idx ON prelo_app.user_recent_items (user_id, viewed_at DESC);

CREATE INDEX projects_search_idx ON prelo_app.projects
    USING gin (prelo_app.search_norm(name || ' ' || coalesce(description, '')) prelo_app.gin_trgm_ops);
CREATE INDEX tasks_search_idx ON prelo_app.tasks USING gin (prelo_app.search_norm(description) prelo_app.gin_trgm_ops);
CREATE INDEX project_files_search_idx ON prelo_app.project_files USING gin (prelo_app.search_norm(name) prelo_app.gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS prelo_app.project_files_search_idx;
DROP INDEX IF EXISTS prelo_app.tasks_search_idx;
DROP INDEX IF EXISTS prelo_app.projects_search_idx;
DROP TABLE IF EXISTS prelo_app.user_recent_items;
ALTER TABLE prelo_app.tasks DROP COLUMN IF EXISTS client_id;
ALTER TABLE prelo_app.projects DROP COLUMN IF EXISTS client_id;
DROP TABLE IF EXISTS prelo_app.client_contacts;
DROP TABLE IF EXISTS prelo_app.clients;
DROP FUNCTION IF EXISTS prelo_app.search_norm(text);
