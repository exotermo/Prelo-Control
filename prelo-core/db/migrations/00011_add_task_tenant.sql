-- +goose Up
-- Tenant ownership for authenticated API tasks. Existing legacy rows remain NULL and are
-- intentionally not visible through tenant-scoped HTTP lookups until explicitly migrated.
ALTER TABLE prelo_app.tasks ADD COLUMN IF NOT EXISTS tenant_id UUID;
CREATE INDEX IF NOT EXISTS tasks_tenant_id_created_at_idx
    ON prelo_app.tasks (tenant_id, created_at DESC)
    WHERE tenant_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS prelo_app.tasks_tenant_id_created_at_idx;
ALTER TABLE prelo_app.tasks DROP COLUMN IF EXISTS tenant_id;
