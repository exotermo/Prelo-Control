-- +goose Up
-- Capacity waits retain the approved request but do not hold a lease. Unsupported profiles are
-- terminal for this worker and must be surfaced to the suspended tool call.
ALTER TABLE prelo_app.executor_jobs DROP CONSTRAINT executor_jobs_status_check;
ALTER TABLE prelo_app.executor_jobs ADD CONSTRAINT executor_jobs_status_check
    CHECK (status IN ('READY','WAITING_FOR_CAPACITY','UNSUPPORTED_CAPACITY','CLAIMED','RUNNING','SUCCEEDED','FAILED','CANCELLED'));
DROP INDEX IF EXISTS prelo_app.executor_jobs_claim_idx;
CREATE INDEX executor_jobs_claim_idx ON prelo_app.executor_jobs(worker_id, created_at)
    WHERE status IN ('READY','WAITING_FOR_CAPACITY','CLAIMED');
ALTER TABLE prelo_app.executor_jobs ADD COLUMN task_notified_at TIMESTAMPTZ;

-- +goose Down
UPDATE prelo_app.executor_jobs SET status='FAILED',result_json='{"code":"capacity_state_rollback"}'::jsonb
    WHERE status IN ('WAITING_FOR_CAPACITY','UNSUPPORTED_CAPACITY');
DROP INDEX IF EXISTS prelo_app.executor_jobs_claim_idx;
ALTER TABLE prelo_app.executor_jobs DROP COLUMN IF EXISTS task_notified_at;
ALTER TABLE prelo_app.executor_jobs DROP CONSTRAINT executor_jobs_status_check;
ALTER TABLE prelo_app.executor_jobs ADD CONSTRAINT executor_jobs_status_check
    CHECK (status IN ('READY','CLAIMED','RUNNING','SUCCEEDED','FAILED','CANCELLED'));
CREATE INDEX executor_jobs_claim_idx ON prelo_app.executor_jobs(worker_id, created_at)
    WHERE status IN ('READY','CLAIMED');
