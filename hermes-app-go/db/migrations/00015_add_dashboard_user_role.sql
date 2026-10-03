-- +goose Up
-- RBAC for hermes-dashboard (see domain.DashboardRole). Every existing user predates this
-- distinction and defaults to ADMIN, so nobody already using the dashboard loses access.
ALTER TABLE hermes_app.dashboard_users ADD COLUMN IF NOT EXISTS role VARCHAR(16) NOT NULL DEFAULT 'ADMIN';

-- +goose Down
ALTER TABLE hermes_app.dashboard_users DROP COLUMN IF EXISTS role;
