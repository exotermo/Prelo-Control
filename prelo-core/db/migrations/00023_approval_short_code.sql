-- +goose Up
-- Fase T: a short code per approval, so the owner can answer "SIM K7Q2" on WhatsApp. Unique only
-- among PENDING requests (codes are recycled once decided/expired).
ALTER TABLE prelo_app.approval_requests ADD COLUMN short_code VARCHAR(8);
CREATE UNIQUE INDEX approval_requests_pending_code_uk ON prelo_app.approval_requests (short_code)
    WHERE status = 'PENDING' AND short_code IS NOT NULL;
CREATE INDEX approval_requests_short_code_idx ON prelo_app.approval_requests (short_code, requested_at DESC)
    WHERE short_code IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS prelo_app.approval_requests_short_code_idx;
DROP INDEX IF EXISTS prelo_app.approval_requests_pending_code_uk;
ALTER TABLE prelo_app.approval_requests DROP COLUMN IF EXISTS short_code;
