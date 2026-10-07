package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type ExecutionTurnRepository struct {
	pool *pgxpool.Pool
}

func NewExecutionTurnRepository(pool *pgxpool.Pool) *ExecutionTurnRepository {
	return &ExecutionTurnRepository{pool: pool}
}

// Insert relies on execution_turns_request_id_uk (and the (execution_id, turn_number) unique
// index) to reject a genuine duplicate outright — callers are expected to have already checked
// FindByRequestID before doing any external call for a turn, so a conflict here signals a bug in
// the loop, not a normal path to swallow silently.
func (r *ExecutionTurnRepository) Insert(ctx context.Context, turn domain.ExecutionTurn) (domain.ExecutionTurn, error) {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO execution_turns
			(id, execution_id, turn_number, kind, request_id, input, output, error, started_at, completed_at,
			 model_profile,task_kind,estimated_context_tokens,input_tokens,output_tokens,duration_ms)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),NULLIF($12,''),NULLIF($13,0),NULLIF($14,0),NULLIF($15,0),NULLIF($16,0))`,
		turn.ID.Value, turn.ExecutionID.Value, turn.TurnNumber, string(turn.Kind), turn.RequestID,
		turn.Input, turn.Output, turn.Error, turn.StartedAt, turn.CompletedAt, turn.ModelProfile, turn.TaskKind,
		turn.EstimatedContextTokens, turn.InputTokens, turn.OutputTokens, turn.DurationMs)
	if err != nil {
		return domain.ExecutionTurn{}, err
	}
	return turn, nil
}

func (r *ExecutionTurnRepository) Update(ctx context.Context, turn domain.ExecutionTurn) (domain.ExecutionTurn, error) {
	_, err := r.pool.Exec(ctx, `
		UPDATE execution_turns SET output = $2, error = $3, completed_at = $4,
			model_profile=NULLIF($5,''),task_kind=NULLIF($6,''),estimated_context_tokens=NULLIF($7,0),
			input_tokens=NULLIF($8,0),output_tokens=NULLIF($9,0),duration_ms=NULLIF($10,0)
		 WHERE id = $1`,
		turn.ID.Value, turn.Output, turn.Error, turn.CompletedAt, turn.ModelProfile, turn.TaskKind,
		turn.EstimatedContextTokens, turn.InputTokens, turn.OutputTokens, turn.DurationMs)
	if err != nil {
		return domain.ExecutionTurn{}, err
	}
	return turn, nil
}

func (r *ExecutionTurnRepository) ListByExecution(ctx context.Context, executionID domain.ExecutionID) ([]domain.ExecutionTurn, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, execution_id, turn_number, kind, request_id, input, output, error, started_at, completed_at,
		       coalesce(model_profile,''),coalesce(task_kind,''),coalesce(estimated_context_tokens,0),coalesce(input_tokens,0),
	       coalesce(output_tokens,0),coalesce(duration_ms,0)
		  FROM execution_turns WHERE execution_id = $1 ORDER BY turn_number`, executionID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []domain.ExecutionTurn
	for rows.Next() {
		turn, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

func (r *ExecutionTurnRepository) FindByRequestID(ctx context.Context, requestID string) (domain.ExecutionTurn, bool, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, execution_id, turn_number, kind, request_id, input, output, error, started_at, completed_at,
	       coalesce(model_profile,''),coalesce(task_kind,''),coalesce(estimated_context_tokens,0),coalesce(input_tokens,0),
	       coalesce(output_tokens,0),coalesce(duration_ms,0)
		  FROM execution_turns WHERE request_id = $1`, requestID)
	turn, err := scanTurn(row)
	if err != nil {
		if errors.Is(err, application.ErrExecutionTurnNotFound) {
			return domain.ExecutionTurn{}, false, nil
		}
		return domain.ExecutionTurn{}, false, err
	}
	return turn, true, nil
}

func scanTurn(row pgx.Row) (domain.ExecutionTurn, error) {
	var t domain.ExecutionTurn
	var kind string
	if err := row.Scan(&t.ID.Value, &t.ExecutionID.Value, &t.TurnNumber, &kind, &t.RequestID,
		&t.Input, &t.Output, &t.Error, &t.StartedAt, &t.CompletedAt, &t.ModelProfile, &t.TaskKind,
		&t.EstimatedContextTokens, &t.InputTokens, &t.OutputTokens, &t.DurationMs); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ExecutionTurn{}, application.ErrExecutionTurnNotFound
		}
		return domain.ExecutionTurn{}, err
	}
	t.Kind = domain.TurnKind(kind)
	return t, nil
}
