-- Fase T: the outbox also carries messages the bridge starts itself (approval requests to the
-- owner, an approved agent message to a client), which answer no inbound message. message_id stays
-- UNIQUE — it is still the idempotency key sent to messaging-core — but no longer has to exist in
-- inbound_events.
ALTER TABLE outbound_replies DROP CONSTRAINT IF EXISTS outbound_replies_message_id_fkey;
