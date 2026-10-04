-- +goose Up
-- Fase C2: who sent a WhatsApp message (a phone in E.164, or a WhatsApp ID such as
-- "123…@lid" when WhatsApp hides the number), so the task can be tied to a client — now or
-- later, when the contact is added to a client.
ALTER TABLE prelo_app.tasks ADD COLUMN contact_address VARCHAR(80);
CREATE INDEX tasks_contact_address_idx ON prelo_app.tasks (contact_address) WHERE contact_address IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS prelo_app.tasks_contact_address_idx;
ALTER TABLE prelo_app.tasks DROP COLUMN IF EXISTS contact_address;
