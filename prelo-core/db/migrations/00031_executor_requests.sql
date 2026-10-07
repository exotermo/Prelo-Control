-- +goose Up
-- A project administrator is a member with an additional, narrowly scoped authority.
-- Only a global ADMIN may assign/revoke this role.
ALTER TABLE prelo_app.project_members ADD COLUMN role VARCHAR(16) NOT NULL DEFAULT 'MEMBER'
    CHECK (role IN ('MEMBER', 'PROJECT_ADMIN'));

-- Approval and queue live in Prelo. The worker token can only read/claim/report its own rows.
CREATE TABLE prelo_app.executor_requests (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES prelo_app.projects(id),
    task_id UUID NOT NULL REFERENCES prelo_app.tasks(id),
    execution_id UUID NOT NULL REFERENCES prelo_app.task_executions(id),
    worker_id UUID NOT NULL REFERENCES prelo_app.executor_workers(id),
    image_digest VARCHAR(71) NOT NULL,
    operation VARCHAR(24) NOT NULL CHECK (operation IN ('START_WORKSPACE','LIST','READ','MKDIR','CREATE')),
    tool_name VARCHAR(200) NOT NULL,
    args_json JSONB NOT NULL,
    payload_hash VARCHAR(71) NOT NULL,
    policy_version BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','APPROVED','DENIED','EXPIRED')),
    requested_by VARCHAR(255) NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    decided_by UUID REFERENCES prelo_app.dashboard_users(id),
    decided_at TIMESTAMPTZ,
    start_before TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 0,
    CHECK ((status='PENDING' AND decided_by IS NULL AND decided_at IS NULL AND start_before IS NULL)
        OR (status='APPROVED' AND decided_by IS NOT NULL AND decided_at IS NOT NULL AND start_before IS NOT NULL)
        OR (status IN ('DENIED','EXPIRED') AND start_before IS NULL))
);
CREATE INDEX executor_requests_project_idx ON prelo_app.executor_requests(project_id, requested_at DESC);
CREATE INDEX executor_requests_pending_idx ON prelo_app.executor_requests(expires_at) WHERE status='PENDING';

CREATE TABLE prelo_app.executor_jobs (
    id UUID PRIMARY KEY,
    request_id UUID NOT NULL UNIQUE REFERENCES prelo_app.executor_requests(id),
    worker_id UUID NOT NULL REFERENCES prelo_app.executor_workers(id),
    status VARCHAR(16) NOT NULL CHECK (status IN ('READY','CLAIMED','RUNNING','SUCCEEDED','FAILED','CANCELLED')),
    attempt INTEGER NOT NULL DEFAULT 0,
    lease_id UUID,
    lease_expires_at TIMESTAMPTZ,
    result_sequence INTEGER NOT NULL DEFAULT 0,
    result_json JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX executor_jobs_claim_idx ON prelo_app.executor_jobs(worker_id, created_at)
    WHERE status IN ('READY','CLAIMED');

-- Project file metadata for worker ingestion. Existing rows remain ordinary human uploads.
ALTER TABLE prelo_app.project_files
    ADD COLUMN relative_path VARCHAR(1024),
    ADD COLUMN origin_task_id UUID REFERENCES prelo_app.tasks(id),
    ADD COLUMN origin_execution_id UUID REFERENCES prelo_app.task_executions(id),
    ADD COLUMN executor_request_id UUID REFERENCES prelo_app.executor_requests(id);
CREATE UNIQUE INDEX project_files_executor_path_uk ON prelo_app.project_files(executor_request_id, relative_path)
    WHERE executor_request_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS prelo_app.project_files_executor_path_uk;
ALTER TABLE prelo_app.project_files DROP COLUMN IF EXISTS executor_request_id,
    DROP COLUMN IF EXISTS origin_execution_id, DROP COLUMN IF EXISTS origin_task_id,
    DROP COLUMN IF EXISTS relative_path;
DROP TABLE IF EXISTS prelo_app.executor_jobs;
DROP TABLE IF EXISTS prelo_app.executor_requests;
ALTER TABLE prelo_app.project_members DROP COLUMN IF EXISTS role;
