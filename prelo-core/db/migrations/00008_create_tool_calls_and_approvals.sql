-- +goose Up
-- Etapa 7/8 (ADR-004): every tool invocation is audited here, allowed or not, and every
-- REQUIRE_APPROVAL decision gets exactly one row in approval_requests tied to it.
CREATE TABLE prelo_app.tool_calls (
    id             UUID PRIMARY KEY,
    task_id        UUID NOT NULL REFERENCES prelo_app.tasks(id),
    execution_id   UUID NOT NULL REFERENCES prelo_app.task_executions(id),
    agent_id       VARCHAR(200) NOT NULL,
    tool_name      VARCHAR(200) NOT NULL,
    args_json      TEXT NOT NULL,
    risk_level     VARCHAR(20) NOT NULL,
    decision       VARCHAR(30) NOT NULL,
    outcome        VARCHAR(20),
    result         TEXT,
    error          VARCHAR(1000),
    created_at     TIMESTAMPTZ NOT NULL,
    resolved_at    TIMESTAMPTZ,
    call_version   BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX tool_calls_execution_idx ON prelo_app.tool_calls(execution_id);

CREATE TABLE prelo_app.approval_requests (
    id               UUID PRIMARY KEY,
    tool_call_id     UUID NOT NULL REFERENCES prelo_app.tool_calls(id),
    scope            TEXT NOT NULL,
    status           VARCHAR(20) NOT NULL,
    requested_at     TIMESTAMPTZ NOT NULL,
    expires_at       TIMESTAMPTZ NOT NULL,
    decided_at       TIMESTAMPTZ,
    decided_by       VARCHAR(200),
    approval_version BIGINT NOT NULL DEFAULT 0
);

-- One call, at most one approval request — InvokeToolUseCase creates it exactly once per call.
CREATE UNIQUE INDEX approval_requests_tool_call_uk ON prelo_app.approval_requests(tool_call_id);
CREATE INDEX approval_requests_pending_idx ON prelo_app.approval_requests (expires_at) WHERE status = 'PENDING';

-- +goose Down
DROP TABLE IF EXISTS prelo_app.approval_requests;
DROP TABLE IF EXISTS prelo_app.tool_calls;
