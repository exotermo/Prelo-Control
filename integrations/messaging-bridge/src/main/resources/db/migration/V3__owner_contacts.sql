-- Fase G2: BRIDGE_OWNER_CONTACTS moves from a static env var to this table, managed through the
-- hermes-dashboard Configurações page (proxied by hermes-go's authenticated
-- /api/v1/settings/owner-contacts, which calls this service's existing admin-token-gated API).
CREATE TABLE owner_contacts (
    phone_e164 TEXT PRIMARY KEY,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by   TEXT
);
