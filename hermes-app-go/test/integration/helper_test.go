package integration

import (
	"context"
	_ "embed"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/exotermo/hermes-app-go/internal/infrastructure/persistence/postgres"
)

//go:embed testdata/baseline_schema.sql
var baselineSchema string

// newTestPool boots a throwaway Postgres container, creates the hermes_app schema, applies
// the V2-V6 baseline (owned by the Java service in real deployments) plus this service's own
// V7+ goose migrations, and returns a pool with search_path already set to hermes_app.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("hermes"),
		tcpostgres.WithUsername("hermes"),
		tcpostgres.WithPassword("hermes"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	baseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer admin.Close()

	if _, err := admin.Exec(ctx, "CREATE SCHEMA hermes_app"); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
	if _, err := admin.Exec(ctx, "SET search_path TO hermes_app"); err != nil {
		t.Fatalf("failed to set search_path: %v", err)
	}
	if _, err := admin.Exec(ctx, baselineSchema); err != nil {
		t.Fatalf("failed to apply baseline schema: %v", err)
	}

	schemaURL := baseURL + "&search_path=hermes_app"
	if err := postgres.Migrate(schemaURL); err != nil {
		t.Fatalf("failed to apply goose migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatalf("failed to connect pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
