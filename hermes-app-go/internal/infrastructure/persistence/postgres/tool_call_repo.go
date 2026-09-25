package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

type ToolCallRepository struct {
	pool *pgxpool.Pool
}

func NewToolCallRepository(pool *pgxpool.Pool) *ToolCallRepository {
	return &ToolCallRepository{pool: pool}
}

func (r *ToolCallRepository) Insert(ctx context.Context, call domain.ToolCall) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tool_calls
			(id, task_id, execution_id, agent_id, tool_name, args_json, risk_level, decision,
			 outcome, result, error, created_at, resolved_at, call_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		call.ID.Value, call.TaskID.Value, call.ExecutionID.Value, call.AgentID.Value, call.ToolName,
		call.ArgsJSON, string(call.RiskLevel), string(call.Decision), outcomeOrNil(call.Outcome),
		call.Result, call.Error, call.CreatedAt, call.ResolvedAt, call.Version)
	return err
}

func (r *ToolCallRepository) FindByID(ctx context.Context, id domain.ToolCallID) (domain.ToolCall, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, execution_id, agent_id, tool_name, args_json, risk_level, decision,
		       outcome, result, error, created_at, resolved_at, call_version
		  FROM tool_calls WHERE id = $1`, id.Value)
	return scanToolCall(row)
}

// Update performs a version-checked write, same discipline as TaskRepository.Update: the
// caller applies a guarded domain transition (ToolCall.Resolved) in-process first.
func (r *ToolCallRepository) Update(ctx context.Context, call domain.ToolCall) (domain.ToolCall, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE tool_calls
		   SET decision = $2, outcome = $3, result = $4, error = $5, resolved_at = $6,
		       call_version = call_version + 1
		 WHERE id = $1 AND call_version = $7
		 RETURNING call_version`,
		call.ID.Value, string(call.Decision), outcomeOrNil(call.Outcome), call.Result, call.Error,
		call.ResolvedAt, call.Version)

	var newVersion int64
	if err := row.Scan(&newVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ToolCall{}, application.ErrOptimisticLock
		}
		return domain.ToolCall{}, err
	}
	return call.WithVersion(newVersion), nil
}

func outcomeOrNil(outcome *domain.ToolCallOutcome) *string {
	if outcome == nil {
		return nil
	}
	s := string(*outcome)
	return &s
}

func scanToolCall(row pgx.Row) (domain.ToolCall, error) {
	var c domain.ToolCall
	var riskLevel, decision string
	var outcome *string
	var agentID string
	if err := row.Scan(&c.ID.Value, &c.TaskID.Value, &c.ExecutionID.Value, &agentID, &c.ToolName,
		&c.ArgsJSON, &riskLevel, &decision, &outcome, &c.Result, &c.Error, &c.CreatedAt,
		&c.ResolvedAt, &c.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ToolCall{}, application.ErrToolCallNotFound
		}
		return domain.ToolCall{}, err
	}
	c.AgentID = domain.AgentID{Value: agentID}
	c.RiskLevel = domain.RiskLevel(riskLevel)
	c.Decision = domain.PermissionDecision(decision)
	if outcome != nil {
		o := domain.ToolCallOutcome(*outcome)
		c.Outcome = &o
	}
	return c, nil
}
