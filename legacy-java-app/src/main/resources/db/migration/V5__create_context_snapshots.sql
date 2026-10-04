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
