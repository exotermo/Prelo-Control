-- +goose Up
-- Fase C (orquestrador multi-agente): a Task created by delegate_to_agent points back at its
-- parent and carries its own depth in the delegation chain — domain.NewSubtask enforces
-- MaxDelegationDepth in application code; this column has no CHECK constraint mirroring that,
-- consistent with how execution_jobs.status also has no DB-level enum.
ALTER TABLE tasks ADD COLUMN parent_task_id UUID REFERENCES tasks(id);
ALTER TABLE tasks ADD COLUMN depth INT NOT NULL DEFAULT 0;

CREATE INDEX tasks_parent_task_id_idx ON tasks(parent_task_id) WHERE parent_task_id IS NOT NULL;

-- +goose Down
ALTER TABLE tasks DROP COLUMN depth;
ALTER TABLE tasks DROP COLUMN parent_task_id;
