CREATE TABLE hermes_llm_executions (
    id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    request_id VARCHAR(100) NOT NULL,
    task_id VARCHAR(100),
    agent_id VARCHAR(100),
    requested_model VARCHAR(255) NOT NULL,
    provider VARCHAR(100),
    duration_ms BIGINT,
    status VARCHAR(32) NOT NULL
);
CREATE INDEX hermes_llm_executions_request_id_idx ON hermes_llm_executions(request_id);
