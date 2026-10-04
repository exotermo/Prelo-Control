-- +goose Up
-- 00013 defaulted every existing row to MANUAL, which is wrong for rows that were actually
-- created by prelo-messaging-bridge before this distinction existed. agent_id = 'customer' is
-- never used by a human (the dashboard's "Nova task" form defaults to/suggests 'general'; only
-- InboundMessageHandler assigns 'customer', for a non-owner WhatsApp sender) — this is the one
-- unambiguous signal available to backfill. 'general' rows are left MANUAL: some really are the
-- owner's own WhatsApp messages (also routed to 'general'), but there is no reliable way to tell
-- those apart from real designated work after the fact, so this errs toward not hiding something
-- someone actually asked for.
UPDATE prelo_app.tasks SET source = 'MESSAGING' WHERE agent_id = 'customer';

-- +goose Down
-- Not reversible with certainty (see above) — leaves rows as MESSAGING rather than guessing
-- which were ever MANUAL, since none were.
