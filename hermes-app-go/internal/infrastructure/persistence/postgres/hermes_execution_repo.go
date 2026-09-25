package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/hermes-app-go/internal/application"
)

// HermesExecutionRepository mirrors HermesExecutionRepository.java / the HermesExecution
// JPA entity — an audit row for every /hermes/chat call, table hermes_llm_executions
// (Flyway V1, owned by the Java service, already present in the shared schema).
type HermesExecutionRepository struct {
	pool *pgxpool.Pool
}

func NewHermesExecutionRepository(pool *pgxpool.Pool) *HermesExecutionRepository {
	return &HermesExecutionRepository{pool: pool}
}

func (r *HermesExecutionRepository) Save(ctx context.Context, record application.HermesExecutionRecord) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO hermes_llm_executions
			(id, created_at, request_id, task_id, agent_id, requested_model, provider, duration_ms, status)
		VALUES (gen_random_uuid(), now(), $1, $2, $3, $4, $5, $6, $7)`,
		record.RequestID, record.TaskID, record.AgentID, record.RequestedModel,
		record.Provider, record.DurationMs, record.Status)
	return err
}
