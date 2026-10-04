package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
)

// LlmExecutionRepository mirrors LlmExecutionRepository.java / the LlmExecution
// JPA entity — an audit row for every /chat call, table llm_executions
// (Flyway V1, owned by the Java service, already present in the shared schema).
type LlmExecutionRepository struct {
	pool *pgxpool.Pool
}

func NewLlmExecutionRepository(pool *pgxpool.Pool) *LlmExecutionRepository {
	return &LlmExecutionRepository{pool: pool}
}

func (r *LlmExecutionRepository) Save(ctx context.Context, record application.LlmExecutionRecord) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO llm_executions
			(id, created_at, request_id, task_id, agent_id, requested_model, provider, duration_ms, status)
		VALUES (gen_random_uuid(), now(), $1, $2, $3, $4, $5, $6, $7)`,
		record.RequestID, record.TaskID, record.AgentID, record.RequestedModel,
		record.Provider, record.DurationMs, record.Status)
	return err
}
