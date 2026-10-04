-- Mirrors Flyway V2-V6 from legacy-java-app (Java), owned there — reproduced here only so
-- integration tests can stand up a full prelo_app schema in a throwaway testcontainers
-- Postgres without depending on the Java service. V1 (legacy llm_executions) is
-- intentionally not reproduced.

CREATE TABLE tasks (id UUID PRIMARY KEY, description VARCHAR(4000) NOT NULL, status VARCHAR(32) NOT NULL, created_at TIMESTAMPTZ NOT NULL);
ALTER TABLE tasks ADD COLUMN tenant_id UUID;
CREATE TABLE task_executions (id UUID PRIMARY KEY, task_id UUID NOT NULL REFERENCES tasks(id), agent_id VARCHAR(100) NOT NULL, status VARCHAR(32) NOT NULL, started_at TIMESTAMPTZ, completed_at TIMESTAMPTZ, error VARCHAR(1000));

ALTER TABLE task_executions
    ADD COLUMN result TEXT,
    ADD COLUMN request_id VARCHAR(100),
    ADD COLUMN model VARCHAR(255),
    ADD COLUMN provider VARCHAR(100),
    ADD COLUMN context_snapshot_id UUID,
    ADD COLUMN agent_version VARCHAR(50),
    ADD COLUMN execution_version BIGINT NOT NULL DEFAULT 0;

ALTER TABLE tasks
    ADD COLUMN task_version BIGINT NOT NULL DEFAULT 0;

CREATE TABLE task_manual_context_items (
    id UUID PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES tasks(id),
    name VARCHAR(200) NOT NULL,
    content TEXT NOT NULL,
    item_order INT NOT NULL
);
CREATE INDEX task_manual_context_items_task_id_idx ON task_manual_context_items(task_id);

CREATE TABLE context_snapshots (
    id UUID PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES tasks(id),
    version INT NOT NULL,
    resolved_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX context_snapshots_task_id_idx ON context_snapshots(task_id);

CREATE TABLE context_snapshot_items (
    id UUID PRIMARY KEY,
    snapshot_id UUID NOT NULL REFERENCES context_snapshots(id),
    name VARCHAR(200) NOT NULL,
    content TEXT NOT NULL,
    source_type VARCHAR(32) NOT NULL,
    provenance VARCHAR(200) NOT NULL,
    item_order INT NOT NULL
);
CREATE INDEX context_snapshot_items_snapshot_id_idx ON context_snapshot_items(snapshot_id);

ALTER TABLE task_executions
    ADD CONSTRAINT fk_task_executions_context_snapshot FOREIGN KEY (context_snapshot_id) REFERENCES context_snapshots(id);

ALTER TABLE tasks ADD COLUMN agent_id VARCHAR(100);
UPDATE tasks SET agent_id = 'general' WHERE agent_id IS NULL;
ALTER TABLE tasks ALTER COLUMN agent_id SET NOT NULL;
