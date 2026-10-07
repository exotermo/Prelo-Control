-- +goose Up

-- Aggregate per-call telemetry supports transparent estimates without storing extra prompt data.
ALTER TABLE prelo_app.execution_turns
    ADD COLUMN model_profile VARCHAR(120),
    ADD COLUMN task_kind VARCHAR(24),
    ADD COLUMN estimated_context_tokens INTEGER,
    ADD COLUMN input_tokens INTEGER,
    ADD COLUMN output_tokens INTEGER,
    ADD COLUMN duration_ms BIGINT;

CREATE INDEX execution_turns_estimate_sample_idx
    ON prelo_app.execution_turns(model_profile, task_kind, estimated_context_tokens, started_at DESC)
    WHERE kind='LLM_CALL' AND input_tokens IS NOT NULL AND output_tokens IS NOT NULL AND duration_ms IS NOT NULL;

CREATE TABLE prelo_app.executor_worker_capacity (
    worker_id UUID PRIMARY KEY REFERENCES prelo_app.executor_workers(id) ON DELETE CASCADE,
    profile_id VARCHAR(80) NOT NULL,
    memory_bytes BIGINT NOT NULL,
    cpu_quota_milli BIGINT NOT NULL,
    disk_bytes BIGINT NOT NULL,
    pids BIGINT NOT NULL,
    available_slots INTEGER NOT NULL,
    maximum_slots INTEGER NOT NULL,
    active_containers INTEGER NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS prelo_app.executor_worker_capacity;
DROP INDEX IF EXISTS prelo_app.execution_turns_estimate_sample_idx;
ALTER TABLE prelo_app.execution_turns
    DROP COLUMN IF EXISTS model_profile,
    DROP COLUMN IF EXISTS task_kind,
    DROP COLUMN IF EXISTS estimated_context_tokens,
    DROP COLUMN IF EXISTS input_tokens,
    DROP COLUMN IF EXISTS output_tokens,
    DROP COLUMN IF EXISTS duration_ms;
