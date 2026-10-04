ALTER TABLE task_executions
    ADD COLUMN result TEXT,
    ADD COLUMN request_id VARCHAR(100),
    ADD COLUMN model VARCHAR(255),
    ADD COLUMN provider VARCHAR(100),
    ADD COLUMN context_snapshot_id UUID,
    ADD COLUMN agent_version VARCHAR(50),
    ADD COLUMN execution_version BIGINT NOT NULL DEFAULT 0;
