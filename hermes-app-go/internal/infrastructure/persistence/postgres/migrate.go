package postgres

import (
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	hermesdb "github.com/exotermo/hermes-app-go/db"
)

// Migrate applies db/migrations/*.sql, continuing the numbering of the existing Flyway
// migrations V1-V6 (owned by the Java hermes-app) in the same hermes_app schema. It tracks
// applied versions in its own history table so it never collides with Flyway's
// hermes_flyway_schema_history.
func Migrate(databaseURL string) error {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	goose.SetTableName("hermes_app.hermes_go_schema_history")
	goose.SetBaseFS(hermesdb.Migrations)

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(sqlDB, "migrations")
}
