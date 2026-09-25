-- +goose Up
-- Durable execution queue (etapa 6.5). Continues the numbering of the existing Flyway
-- migrations V1-V6 (owned by the Java hermes-app) in the same hermes_app schema; tracked by
-- goose in its own history table (hermes_go_schema_history) so it doesn't collide with
-- Flyway's hermes_flyway_schema_history.
CREATE TABLE hermes_app.execution_jobs (
    id                 UUID PRIMARY KEY,
    task_id            UUID NOT NULL REFERENCES hermes_app.tasks(id),
    execution_id       UUID NOT NULL REFERENCES hermes_app.task_executions(id),
    status             VARCHAR(32) NOT NULL,
    priority           SMALLINT NOT NULL DEFAULT 0,
    attempt            INT NOT NULL DEFAULT 0,
    max_attempts       INT NOT NULL DEFAULT 5,
    available_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_by         VARCHAR(100),
    claimed_at         TIMESTAMPTZ,
    lease_expires_at   TIMESTAMPTZ,
    last_error         VARCHAR(1000),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    job_version        BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX execution_jobs_execution_id_uk ON hermes_app.execution_jobs(execution_id);

CREATE INDEX execution_jobs_claimable_idx
    ON hermes_app.execution_jobs (available_at)
    WHERE status IN ('PENDING','RETRY');

CREATE INDEX execution_jobs_lease_idx
    ON hermes_app.execution_jobs (lease_expires_at)
    WHERE status IN ('CLAIMED','RUNNING');

-- +goose Down
DROP TABLE IF EXISTS hermes_app.execution_jobs;
