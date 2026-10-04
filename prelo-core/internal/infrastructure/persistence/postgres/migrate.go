package postgres

import (
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	prelodb "github.com/exotermo/prelo-core/db"
)

// Migrate applies db/migrations/*.sql, continuing the numbering of the existing Flyway
// migrations V1-V6 (owned by the Java prelo-core) in the same prelo_app schema. It tracks
// applied versions in its own history table so it never collides with Flyway's
// legacy_flyway_schema_history.
func Migrate(databaseURL string) error {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	goose.SetTableName("prelo_app.prelo_core_schema_history")
	goose.SetBaseFS(prelodb.Migrations)

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(sqlDB, "migrations")
}
