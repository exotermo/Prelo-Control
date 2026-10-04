package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type ExecutionJobRepository struct {
	pool *pgxpool.Pool
}

func NewExecutionJobRepository(pool *pgxpool.Pool) *ExecutionJobRepository {
	return &ExecutionJobRepository{pool: pool}
}

func (r *ExecutionJobRepository) Insert(ctx context.Context, job domain.ExecutionJob) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO execution_jobs
			(id, task_id, execution_id, status, priority, attempt, max_attempts,
			 available_at, created_at, updated_at, job_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		job.ID.Value, job.TaskID.Value, job.ExecutionID.Value, string(job.Status), job.Priority,
		job.Attempt, job.MaxAttempts, job.AvailableAt, job.CreatedAt, job.UpdatedAt, job.Version)
	return err
}

func (r *ExecutionJobRepository) FindByID(ctx context.Context, id domain.ExecutionID) (domain.ExecutionJob, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, execution_id, status, priority, attempt, max_attempts, available_at,
		       claimed_by, claimed_at, lease_expires_at, last_error, created_at, updated_at, job_version
		  FROM execution_jobs WHERE id = $1`, id.Value)
	return scanJob(row)
}

// Claim atomically transitions a PENDING/RETRY job to CLAIMED for exactly one caller — a
// belt-and-suspenders status-guarded UPDATE (see ADR-013), not a version-precondition check
// like Task/Execution's Update, since the caller doesn't hold a prior in-memory read to guard
// against; the race is resolved entirely by Postgres row locking during the UPDATE.
func (r *ExecutionJobRepository) Claim(ctx context.Context, id domain.ExecutionID, workerID string, leaseDuration time.Duration) (domain.ExecutionJob, error) {
	// Distinguishes "no such job" (a real ErrJobNotFound) from "job exists but isn't
	// claimable right now" (ErrJobClaimLost) — the UPDATE below can't tell those apart on
	// its own, since both produce zero affected rows.
	if _, err := r.FindByID(ctx, id); err != nil {
		return domain.ExecutionJob{}, err
	}

	now := time.Now().UTC()
	leaseExpiresAt := now.Add(leaseDuration)

	row := r.pool.QueryRow(ctx, `
		UPDATE execution_jobs
		   SET status = 'CLAIMED', claimed_by = $2, claimed_at = $3, lease_expires_at = $4,
		       updated_at = $3, job_version = job_version + 1
		 WHERE id = $1 AND status IN ('PENDING','RETRY')
		 RETURNING id, task_id, execution_id, status, priority, attempt, max_attempts, available_at,
		           claimed_by, claimed_at, lease_expires_at, last_error, created_at, updated_at, job_version`,
		id.Value, workerID, now, leaseExpiresAt)

	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, application.ErrJobNotFound) {
			return domain.ExecutionJob{}, application.ErrJobClaimLost
		}
		return domain.ExecutionJob{}, err
	}
	return job, nil
}

// MarkRunning also renews the lease (leaseDuration from now) — it used to only flip the status
// and leave lease_expires_at exactly where Claim set it, which was fine while a single Gateway
// call was the whole job body (etapa 6.5) but is not enough once one job can mean up to
// maxLLMCalls sequential Gateway calls plus tool executions (Fase B): a long-running loop could
// outlive its original lease, and RequeueOrphaned would then hand the *same* job to a second
// worker while the first is still legitimately working. Guarded to only leave CLAIMED — if
// another actor already moved it away (e.g. the sweeper decided this worker was dead), this
// worker has lost the job and must stop, not keep calling the Gateway.
func (r *ExecutionJobRepository) MarkRunning(ctx context.Context, id domain.ExecutionID, leaseDuration time.Duration) error {
	leaseExpiresAt := time.Now().UTC().Add(leaseDuration)
	tag, err := r.pool.Exec(ctx, `
		UPDATE execution_jobs
		   SET status = 'RUNNING', lease_expires_at = $2, updated_at = now(), job_version = job_version + 1
		 WHERE id = $1 AND status = 'CLAIMED'`, id.Value, leaseExpiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrJobClaimLost
	}
	return nil
}

// MarkDone is guarded to only leave RUNNING — without this, a "zombie" worker whose lease
// already expired (the sweeper moved the job to RETRY/DEAD, possibly already reclaimed and
// completed by a second worker) could silently overwrite a newer, unrelated outcome when it
// finally gets around to finishing its own stale attempt.
func (r *ExecutionJobRepository) MarkDone(ctx context.Context, id domain.ExecutionID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE execution_jobs SET status = 'DONE', updated_at = now(), job_version = job_version + 1
		 WHERE id = $1 AND status = 'RUNNING'`, id.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrJobClaimLost
	}
	return nil
}

// Suspend is guarded to only leave RUNNING — matches the loop's own invariant (only the
// worker actively holding the job can suspend it) without needing a version precondition,
// same "belt and suspenders" status-guarded style as Claim.
func (r *ExecutionJobRepository) Suspend(ctx context.Context, id domain.ExecutionID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE execution_jobs
		   SET status = 'AWAITING_RESUME', claimed_by = NULL, claimed_at = NULL, lease_expires_at = NULL,
		       updated_at = now(), job_version = job_version + 1
		 WHERE id = $1 AND status = 'RUNNING'`, id.Value)
	return err
}

// Resume only leaves AWAITING_RESUME — a job that was never suspended, or already resumed by
// another resolver, is a no-op here, not an error: ExecutionSuspension.Resolve is the actual
// guard against resolving the same suspension twice (see DecideApprovalUseCase).
func (r *ExecutionJobRepository) Resume(ctx context.Context, id domain.ExecutionID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE execution_jobs
		   SET status = 'PENDING', available_at = now(), updated_at = now(), job_version = job_version + 1
		 WHERE id = $1 AND status = 'AWAITING_RESUME'`, id.Value)
	return err
}

// MarkFailed is guarded the same way MarkDone is — see its doc comment.
func (r *ExecutionJobRepository) MarkFailed(ctx context.Context, id domain.ExecutionID, message string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE execution_jobs
		   SET status = 'FAILED', last_error = $2, updated_at = now(), job_version = job_version + 1
		 WHERE id = $1 AND status = 'RUNNING'`, id.Value, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrJobClaimLost
	}
	return nil
}

// ListClaimable returns up to limit job ids currently eligible for processing (PENDING or
// RETRY, with available_at already past). It is deliberately a plain read — no FOR UPDATE
// SKIP LOCKED — since correctness comes entirely from Claim's atomic status-guarded UPDATE;
// two sweepers listing the same id just means one of them loses the subsequent Claim, not a
// double-processed job.
func (r *ExecutionJobRepository) ListClaimable(ctx context.Context, limit int) ([]domain.ExecutionID, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id FROM execution_jobs
		 WHERE status IN ('PENDING','RETRY') AND available_at <= now()
		 ORDER BY priority DESC, created_at ASC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []domain.ExecutionID
	for rows.Next() {
		var id domain.ExecutionID
		if err := rows.Scan(&id.Value); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RequeueOrphaned finds jobs stuck CLAIMED/RUNNING past their lease (the worker that held
// them presumably crashed or was killed) and moves them to RETRY with an incremented attempt
// and a short backoff, or to the terminal DEAD state once max_attempts is reached. This is
// the crash-recovery path (etapa G11): it depends only on Postgres state, never on Redis.
func (r *ExecutionJobRepository) RequeueOrphaned(ctx context.Context) ([]domain.ExecutionID, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE execution_jobs
		   SET status = CASE WHEN attempt + 1 >= max_attempts THEN 'DEAD' ELSE 'RETRY' END,
		       attempt = attempt + 1,
		       available_at = now() + (LEAST(attempt + 1, 6) * interval '10 seconds'),
		       last_error = 'lease expired, requeued by sweeper',
		       updated_at = now(),
		       job_version = job_version + 1
		 WHERE status IN ('CLAIMED','RUNNING') AND lease_expires_at < now()
		 RETURNING id, status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var retried []domain.ExecutionID
	for rows.Next() {
		var id domain.ExecutionID
		var status string
		if err := rows.Scan(&id.Value, &status); err != nil {
			return nil, err
		}
		if status == string(domain.JobRetry) {
			retried = append(retried, id)
		}
	}
	return retried, rows.Err()
}

func scanJob(row pgx.Row) (domain.ExecutionJob, error) {
	var job domain.ExecutionJob
	var status string
	if err := row.Scan(&job.ID.Value, &job.TaskID.Value, &job.ExecutionID.Value, &status, &job.Priority,
		&job.Attempt, &job.MaxAttempts, &job.AvailableAt, &job.ClaimedBy, &job.ClaimedAt,
		&job.LeaseExpiresAt, &job.LastError, &job.CreatedAt, &job.UpdatedAt, &job.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ExecutionJob{}, application.ErrJobNotFound
		}
		return domain.ExecutionJob{}, err
	}
	job.Status = domain.JobStatus(status)
	return job, nil
}
