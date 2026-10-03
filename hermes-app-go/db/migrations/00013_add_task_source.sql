-- +goose Up
-- Distinguishes a human/dashboard-designated task from one hermes-messaging-bridge creates for
-- an inbound message on some channel (today, WhatsApp) — see domain.TaskSource. Existing rows
-- predate this distinction and default to MANUAL, which is the closer approximation for rows
-- created before the bridge integration existed.
ALTER TABLE hermes_app.tasks ADD COLUMN IF NOT EXISTS source VARCHAR(32) NOT NULL DEFAULT 'MANUAL';

-- +goose Down
ALTER TABLE hermes_app.tasks DROP COLUMN IF EXISTS source;
