-- +goose Up
-- Project overrides for curated agent tools. No row preserves the pre-existing catalog behavior.
CREATE TABLE project_tool_settings (
    project_id UUID NOT NULL REFERENCES projects(id),
    tool_name VARCHAR(200) NOT NULL,
    enabled BOOLEAN NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    changed_by VARCHAR(255) NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, tool_name)
);

CREATE TABLE project_tool_setting_events (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id),
    tool_name VARCHAR(200) NOT NULL,
    enabled BOOLEAN NOT NULL,
    version BIGINT NOT NULL,
    changed_by VARCHAR(255) NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX project_tool_setting_events_project_idx ON project_tool_setting_events(project_id, changed_at DESC);

-- Approval requests must keep the exact policy generation under which the tool was asked.
ALTER TABLE tool_calls ADD COLUMN tool_policy_version BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE tool_calls DROP COLUMN IF EXISTS tool_policy_version;
DROP TABLE IF EXISTS project_tool_setting_events;
DROP TABLE IF EXISTS project_tool_settings;
