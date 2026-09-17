CREATE TABLE gateway_audit_events (
    id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    request_id VARCHAR(100),
    subject VARCHAR(255),
    client_id VARCHAR(255),
    provider VARCHAR(100),
    model VARCHAR(255),
    duration_ms BIGINT,
    detail VARCHAR(500)
);
CREATE INDEX gateway_audit_events_request_id_idx ON gateway_audit_events(request_id);
