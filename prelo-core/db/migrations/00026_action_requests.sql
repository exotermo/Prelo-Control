-- +goose Up
-- PR-3 (contratos G5–G8, docs/integracoes/action-requests.md): an external system (BastionDeploy)
-- asks the Prelo to authorize an exact action. The decision is bound to payload_hash; results are
-- reported back and kept as an audit trail.
CREATE TABLE prelo_app.action_requests (
    id                 UUID PRIMARY KEY,
    workspace_id       UUID NOT NULL REFERENCES prelo_app.workspace(id),
    project_id         UUID NOT NULL REFERENCES prelo_app.projects(id),
    kind               VARCHAR(32) NOT NULL CHECK (kind IN ('deploy')),
    payload            JSONB NOT NULL,
    payload_hash       VARCHAR(71) NOT NULL CHECK (payload_hash ~ '^sha256:[0-9a-f]{64}$'),
    risk               VARCHAR(16) NOT NULL CHECK (risk IN ('LOW', 'MODERATE', 'HIGH')),
    impact             VARCHAR(300) NOT NULL,
    requested_by       VARCHAR(200) NOT NULL,
    idempotency_key    VARCHAR(200) NOT NULL,
    api_key_id         UUID REFERENCES prelo_app.api_keys(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    result_status      VARCHAR(16) CHECK (result_status IN ('RUNNING', 'SUCCEEDED', 'FAILED', 'ROLLED_BACK', 'CANCELLED')),
    result_sequence    INTEGER,
    result_message     VARCHAR(500),
    result_url         VARCHAR(500),
    result_digest      VARCHAR(100),
    result_reported_at TIMESTAMPTZ,
    UNIQUE (project_id, idempotency_key)
);
CREATE INDEX action_requests_project_idx ON prelo_app.action_requests (project_id, created_at DESC);

-- Every reported result, in order (audit trail; the row above keeps only the latest).
CREATE TABLE prelo_app.action_request_results (
    action_request_id UUID NOT NULL REFERENCES prelo_app.action_requests(id),
    sequence          INTEGER NOT NULL,
    status            VARCHAR(16) NOT NULL,
    message           VARCHAR(500),
    url               VARCHAR(500),
    digest            VARCHAR(100),
    reported_at       TIMESTAMPTZ NOT NULL,
    received_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (action_request_id, sequence)
);

-- An approval now belongs to exactly one of: an agent's tool call, or an external action request.
ALTER TABLE prelo_app.approval_requests ALTER COLUMN tool_call_id DROP NOT NULL;
ALTER TABLE prelo_app.approval_requests ADD COLUMN action_request_id UUID REFERENCES prelo_app.action_requests(id);
ALTER TABLE prelo_app.approval_requests ADD CONSTRAINT approval_requests_one_subject
    CHECK ((tool_call_id IS NULL) <> (action_request_id IS NULL));
CREATE UNIQUE INDEX approval_requests_action_uk ON prelo_app.approval_requests (action_request_id) WHERE action_request_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS prelo_app.approval_requests_action_uk;
ALTER TABLE prelo_app.approval_requests DROP CONSTRAINT IF EXISTS approval_requests_one_subject;
DELETE FROM prelo_app.approval_requests WHERE action_request_id IS NOT NULL;
ALTER TABLE prelo_app.approval_requests DROP COLUMN IF EXISTS action_request_id;
ALTER TABLE prelo_app.approval_requests ALTER COLUMN tool_call_id SET NOT NULL;
DROP TABLE IF EXISTS prelo_app.action_request_results;
DROP TABLE IF EXISTS prelo_app.action_requests;
