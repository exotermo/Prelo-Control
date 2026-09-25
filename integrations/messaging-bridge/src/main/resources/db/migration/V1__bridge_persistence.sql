-- Fase F: persistência mínima do bridge (idempotência de mensagem + outbox de resposta).
-- Ver docs/adr do repo hermes e renato para o desenho completo (Fase F do plano de hardening).

CREATE TABLE inbound_events (
    message_id          TEXT PRIMARY KEY,
    channel_identity_id TEXT NOT NULL,
    from_address         TEXT NOT NULL,
    raw_payload          TEXT NOT NULL,
    status               TEXT NOT NULL,
    received_at          TIMESTAMPTZ NOT NULL,
    processed_at         TIMESTAMPTZ
);

CREATE TABLE outbound_replies (
    id                   UUID PRIMARY KEY,
    message_id           TEXT NOT NULL UNIQUE REFERENCES inbound_events(message_id),
    channel_identity_id  TEXT NOT NULL,
    to_address           TEXT NOT NULL,
    text                 TEXT NOT NULL,
    status               TEXT NOT NULL,
    attempt              INT NOT NULL DEFAULT 0,
    max_attempts         INT NOT NULL DEFAULT 5,
    available_at         TIMESTAMPTZ NOT NULL,
    claimed_by           TEXT,
    claimed_at           TIMESTAMPTZ,
    lease_expires_at     TIMESTAMPTZ,
    last_error           TEXT,
    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL,
    version              BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX idx_outbound_replies_claimable ON outbound_replies (status, available_at);
