-- Contract v2 callback replay protection. The unique key makes registration atomic across
-- bridge replicas and survives process restarts; cleanup may remove rows after expires_at.
CREATE TABLE webhook_replays (
    webhook_id   TEXT PRIMARY KEY,
    received_at  TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    request_id   TEXT
);

CREATE INDEX webhook_replays_expires_at_idx ON webhook_replays (expires_at);
