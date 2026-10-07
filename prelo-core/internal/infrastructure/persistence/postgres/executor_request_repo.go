package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ExecutorRequestRepository struct{ pool *pgxpool.Pool }

func NewExecutorRequestRepository(pool *pgxpool.Pool) *ExecutorRequestRepository {
	return &ExecutorRequestRepository{pool: pool}
}

const executorRequestColumns = `id,project_id,task_id,execution_id,worker_id,image_digest,operation,tool_name,args_json,payload_hash,
    policy_version,status,requested_by,requested_at,expires_at,decided_by,decided_at,start_before,version,tool_call_id,approval_id`

func scanExecutorRequest(row pgx.Row) (application.ExecutorRequest, error) {
	var q application.ExecutorRequest
	var projectID, taskID, executionID, workerID uuid.UUID
	var args []byte
	var toolName string
	var toolCallID, approvalID pgtype.UUID
	err := row.Scan(&q.ID, &projectID, &taskID, &executionID, &workerID, &q.Payload.ImageDigest, &q.Payload.Operation, &toolName, &args, &q.PayloadHash,
		&q.PolicyVersion, &q.Status, &q.RequestedBy, &q.RequestedAt, &q.ExpiresAt, &q.DecidedBy, &q.DecidedAt, &q.StartBefore, &q.Version, &toolCallID, &approvalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ExecutorRequest{}, application.ErrExecutorRequestNotFound
	}
	if err != nil {
		return application.ExecutorRequest{}, err
	}
	q.Payload.ProjectID = projectID.String()
	q.Payload.TaskID = taskID.String()
	q.Payload.ExecutionID = executionID.String()
	q.Payload.WorkerID = workerID.String()
	if toolCallID.Valid {
		value := uuid.UUID(toolCallID.Bytes)
		q.ToolCallID = &value
	}
	if approvalID.Valid {
		value := uuid.UUID(approvalID.Bytes)
		q.ApprovalID = &value
	}
	if err := json.Unmarshal(args, &q.Payload.Args); err != nil {
		return application.ExecutorRequest{}, err
	}
	return q, nil
}

func (r *ExecutorRequestRepository) LinkToolCall(ctx context.Context, requestID, toolCallID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE executor_requests SET tool_call_id=$2 WHERE id=$1 AND status='PENDING' AND tool_call_id IS NULL`, requestID, toolCallID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return application.ErrExecutorConflict
	}
	return nil
}
func (r *ExecutorRequestRepository) LinkApproval(ctx context.Context, requestID, approvalID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE executor_requests SET approval_id=$2 WHERE id=$1 AND status='PENDING' AND approval_id IS NULL`, requestID, approvalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return application.ErrExecutorConflict
	}
	return nil
}
func (r *ExecutorRequestRepository) FindByToolCall(ctx context.Context, toolCallID uuid.UUID) (application.ExecutorRequest, error) {
	return scanExecutorRequest(r.pool.QueryRow(ctx, `SELECT `+executorRequestColumns+` FROM executor_requests WHERE tool_call_id=$1`, toolCallID))
}
func (r *ExecutorRequestRepository) FindJob(ctx context.Context, jobID uuid.UUID) (application.ExecutorJob, error) {
	var requestID uuid.UUID
	var result []byte
	var leaseID *uuid.UUID
	var status string
	var leaseUntil *time.Time
	var sequence int
	var taskNotifiedAt *time.Time
	err := r.pool.QueryRow(ctx, `SELECT request_id,status,lease_id,lease_expires_at,result_sequence,result_json,task_notified_at FROM executor_jobs WHERE id=$1`, jobID).
		Scan(&requestID, &status, &leaseID, &leaseUntil, &sequence, &result, &taskNotifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ExecutorJob{}, application.ErrExecutorRequestNotFound
	}
	if err != nil {
		return application.ExecutorJob{}, err
	}
	request, err := r.Find(ctx, requestID)
	if err != nil {
		return application.ExecutorJob{}, err
	}
	job := application.ExecutorJob{ID: jobID, Request: request, Status: status, Sequence: sequence, Result: result, TaskNotifiedAt: taskNotifiedAt}
	if leaseID != nil {
		job.LeaseID = *leaseID
	}
	if leaseUntil != nil {
		job.LeaseUntil = *leaseUntil
	}
	return job, nil
}
func (r *ExecutorRequestRepository) MarkTaskNotified(ctx context.Context, jobID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE executor_jobs SET task_notified_at=coalesce(task_notified_at,now()),updated_at=now()
		WHERE id=$1 AND status IN ('SUCCEEDED','FAILED','CANCELLED','UNSUPPORTED_CAPACITY')`, jobID)
	return err
}
func (r *ExecutorRequestRepository) expireRequests(ctx context.Context) error {
	if _, err := r.pool.Exec(ctx, `UPDATE prelo_app.executor_requests SET status='EXPIRED',version=version+1
		WHERE status='PENDING' AND expires_at<=now()`); err != nil {
		return err
	}
	if _, err := r.pool.Exec(ctx, `UPDATE prelo_app.executor_requests q SET status='EXPIRED',start_before=NULL,version=version+1
		WHERE q.status='APPROVED' AND q.start_before<=now() AND EXISTS (
			SELECT 1 FROM prelo_app.executor_jobs j WHERE j.request_id=q.id AND j.status='READY')`); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `UPDATE prelo_app.executor_jobs j SET status='FAILED',result_json='{"code":"approval_window_closed"}'::jsonb,updated_at=now()
		FROM prelo_app.executor_requests q WHERE q.id=j.request_id AND q.status='EXPIRED' AND j.status='READY'`)
	return err
}

func (r *ExecutorRequestRepository) attachJobStatus(ctx context.Context, q *application.ExecutorRequest) error {
	err := r.pool.QueryRow(ctx, `SELECT status,result_json FROM executor_jobs WHERE request_id=$1`, q.ID).
		Scan(&q.JobStatus, &q.JobResult)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}
func (r *ExecutorRequestRepository) Insert(ctx context.Context, q application.ExecutorRequest) error {
	projectID, _ := uuid.Parse(q.Payload.ProjectID)
	taskID, _ := uuid.Parse(q.Payload.TaskID)
	executionID, _ := uuid.Parse(q.Payload.ExecutionID)
	workerID, _ := uuid.Parse(q.Payload.WorkerID)
	args, _ := json.Marshal(q.Payload.Args)
	toolName, _ := application.ExecutorToolName(q.Payload.Operation)
	// The database rechecks every cross-reference to close the gap between service reads and insert.
	tag, err := r.pool.Exec(ctx, `INSERT INTO executor_requests
		(id,project_id,task_id,execution_id,worker_id,image_digest,operation,tool_name,args_json,payload_hash,policy_version,status,requested_by,requested_at,expires_at)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'PENDING',$12,$13,$14
		WHERE EXISTS (SELECT 1 FROM tasks t JOIN task_executions e ON e.task_id=t.id
			JOIN projects p ON p.id=t.project_id JOIN executor_workers w ON w.id=$5
			WHERE t.id=$3 AND e.id=$4 AND p.id=$2 AND p.deleted_at IS NULL
			AND w.project_id=$2 AND w.enabled=TRUE AND w.image_digest=$6
			AND t.status NOT IN ('COMPLETED','FAILED') AND e.status NOT IN ('COMPLETED','FAILED')
			AND EXISTS(SELECT 1 FROM project_tool_settings s WHERE s.project_id=$2 AND s.tool_name=$8 AND s.enabled AND s.version=$11))`,
		q.ID, projectID, taskID, executionID, workerID, q.Payload.ImageDigest, q.Payload.Operation, toolName, args, q.PayloadHash, q.PolicyVersion, q.RequestedBy, q.RequestedAt, q.ExpiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrExecutorUnauthorized
	}
	return nil
}
func (r *ExecutorRequestRepository) Find(ctx context.Context, id uuid.UUID) (application.ExecutorRequest, error) {
	if err := r.expireRequests(ctx); err != nil {
		return application.ExecutorRequest{}, err
	}
	q, err := scanExecutorRequest(r.pool.QueryRow(ctx, `SELECT `+executorRequestColumns+` FROM executor_requests WHERE id=$1`, id))
	if err != nil {
		return application.ExecutorRequest{}, err
	}
	if err := r.attachJobStatus(ctx, &q); err != nil {
		return application.ExecutorRequest{}, err
	}
	return q, nil
}
func (r *ExecutorRequestRepository) ListByProject(ctx context.Context, id domain.ProjectID) ([]application.ExecutorRequest, error) {
	if err := r.expireRequests(ctx); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+executorRequestColumns+` FROM executor_requests WHERE project_id=$1 ORDER BY requested_at DESC LIMIT 100`, id.Value)
	if err != nil {
		return nil, err
	}
	out := []application.ExecutorRequest{}
	for rows.Next() {
		q, err := scanExecutorRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}
	for i := range out {
		if err := r.attachJobStatus(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *ExecutorRequestRepository) Decide(ctx context.Context, id, actor uuid.UUID, approve bool) (application.ExecutorRequest, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.ExecutorRequest{}, err
	}
	defer tx.Rollback(ctx)
	q, err := scanExecutorRequest(tx.QueryRow(ctx, `SELECT `+executorRequestColumns+` FROM executor_requests WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return application.ExecutorRequest{}, err
	}
	if q.Status != "PENDING" || !time.Now().UTC().Before(q.ExpiresAt) {
		return application.ExecutorRequest{}, application.ErrExecutorConflict
	}
	projectID, _ := uuid.Parse(q.Payload.ProjectID)
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dashboard_users u
		LEFT JOIN project_members m ON m.dashboard_user_id=u.id AND m.project_id=$2
		WHERE u.id=$1 AND u.activated_at IS NOT NULL AND (u.role='ADMIN' OR m.role='PROJECT_ADMIN'))`, actor, projectID).Scan(&allowed)
	if err != nil {
		return application.ExecutorRequest{}, err
	}
	if !allowed {
		return application.ExecutorRequest{}, application.ErrExecutorUnauthorized
	}
	status := "DENIED"
	var startBefore *time.Time
	if approve {
		workerID, _ := uuid.Parse(q.Payload.WorkerID)
		toolName, _ := application.ExecutorToolName(q.Payload.Operation)
		var valid bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM executor_workers w JOIN projects p ON p.id=w.project_id
			JOIN tasks t ON t.id=$2 JOIN task_executions e ON e.id=$3 AND e.task_id=t.id
			WHERE w.id=$1 AND w.enabled AND w.project_id=$4 AND w.image_digest=$5 AND p.deleted_at IS NULL
			AND t.project_id=$4 AND t.status NOT IN ('COMPLETED','FAILED') AND e.status NOT IN ('COMPLETED','FAILED')
			AND EXISTS(SELECT 1 FROM project_tool_settings s WHERE s.project_id=$4 AND s.tool_name=$6 AND s.enabled AND s.version=$7))`, workerID, q.Payload.TaskID, q.Payload.ExecutionID, projectID, q.Payload.ImageDigest, toolName, q.PolicyVersion).Scan(&valid)
		if err != nil {
			return application.ExecutorRequest{}, err
		}
		if !valid {
			return application.ExecutorRequest{}, application.ErrExecutorConflict
		}
		status = "APPROVED"
		deadline := time.Now().UTC().Add(5 * time.Minute)
		startBefore = &deadline
	}
	err = tx.QueryRow(ctx, `UPDATE executor_requests SET status=$2,decided_by=$3,decided_at=now(),start_before=$4,version=version+1
		WHERE id=$1 AND status='PENDING' RETURNING decided_at,version`, id, status, actor, startBefore).Scan(&q.DecidedAt, &q.Version)
	if err != nil {
		return application.ExecutorRequest{}, err
	}
	q.Status = status
	q.DecidedBy = &actor
	q.StartBefore = startBefore
	if approve {
		workerID, _ := uuid.Parse(q.Payload.WorkerID)
		_, err = tx.Exec(ctx, `INSERT INTO executor_jobs(id,request_id,worker_id,status) VALUES($1,$2,$3,'READY')`, uuid.New(), id, workerID)
		if err != nil {
			return application.ExecutorRequest{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return application.ExecutorRequest{}, err
	}
	return q, nil
}
func (r *ExecutorRequestRepository) Claim(ctx context.Context, workerID, leaseID uuid.UUID, capacity application.ExecutorCapacity) (*application.ExecutorJob, error) {
	if err := r.expireRequests(ctx); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT enabled FROM executor_workers WHERE id=$1 FOR UPDATE`, workerID).Scan(&enabled); err != nil || !enabled {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, application.ErrExecutorUnauthorized
	}
	if _, err := tx.Exec(ctx, `INSERT INTO executor_worker_capacity
		(worker_id,profile_id,memory_bytes,cpu_quota_milli,disk_bytes,pids,available_slots,maximum_slots,active_containers,observed_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT(worker_id) DO UPDATE SET profile_id=EXCLUDED.profile_id,memory_bytes=EXCLUDED.memory_bytes,
		cpu_quota_milli=EXCLUDED.cpu_quota_milli,disk_bytes=EXCLUDED.disk_bytes,pids=EXCLUDED.pids,
		available_slots=EXCLUDED.available_slots,maximum_slots=EXCLUDED.maximum_slots,
		active_containers=EXCLUDED.active_containers,observed_at=EXCLUDED.observed_at`,
		workerID, capacity.ProfileID, capacity.MemoryBytes, capacity.CPUQuotaMilli, capacity.DiskBytes,
		capacity.PIDs, capacity.AvailableSlots, capacity.MaximumSlots, capacity.ActiveContainers, capacity.ObservedAt); err != nil {
		return nil, err
	}
	activeIDs := capacity.ActiveExecutionIDs
	if len(activeIDs) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE executor_requests q SET start_before=now()+interval '5 minutes',version=version+1
			FROM executor_jobs j WHERE j.request_id=q.id AND j.worker_id=$1 AND j.status='WAITING_FOR_CAPACITY'
			AND q.status='APPROVED' AND q.operation='START_WORKSPACE' AND q.execution_id=ANY($2::uuid[])`, workerID, activeIDs); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE executor_jobs j SET status='READY',result_json=NULL,updated_at=now()
			FROM executor_requests q WHERE q.id=j.request_id AND j.worker_id=$1 AND j.status='WAITING_FOR_CAPACITY'
			AND q.status='APPROVED' AND q.operation='START_WORKSPACE' AND q.execution_id=ANY($2::uuid[])`, workerID, activeIDs); err != nil {
			return nil, err
		}
	}
	if capacity.MaximumSlots == 0 {
		if _, err := tx.Exec(ctx, `UPDATE executor_jobs j SET status='UNSUPPORTED_CAPACITY',
			result_json='{"code":"unsupported_capacity","message":"Este worker não comporta o perfil de recursos desta operação."}'::jsonb,updated_at=now()
			FROM executor_requests q WHERE q.id=j.request_id AND j.worker_id=$1 AND j.status IN ('READY','WAITING_FOR_CAPACITY')
			AND q.status='APPROVED' AND q.operation='START_WORKSPACE' AND NOT (q.execution_id=ANY($2::uuid[]))`, workerID, activeIDs); err != nil {
			return nil, err
		}
	} else if capacity.AvailableSlots == 0 {
		if _, err := tx.Exec(ctx, `UPDATE executor_jobs j SET status='WAITING_FOR_CAPACITY',
			result_json='{"code":"waiting_for_capacity","message":"Aguardando recursos do servidor."}'::jsonb,updated_at=now()
			FROM executor_requests q WHERE q.id=j.request_id AND j.worker_id=$1 AND j.status='READY'
			AND q.status='APPROVED' AND q.operation='START_WORKSPACE' AND NOT (q.execution_id=ANY($2::uuid[]))`, workerID, activeIDs); err != nil {
			return nil, err
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE executor_requests q SET start_before=now()+interval '5 minutes',version=version+1
			FROM executor_jobs j WHERE j.request_id=q.id AND j.worker_id=$1 AND j.status='WAITING_FOR_CAPACITY'
			AND q.status='APPROVED' AND q.operation='START_WORKSPACE'`, workerID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE executor_jobs j SET status='READY',result_json=NULL,updated_at=now()
			FROM executor_requests q WHERE q.id=j.request_id AND j.worker_id=$1 AND j.status='WAITING_FOR_CAPACITY'
			AND q.status='APPROVED' AND q.operation='START_WORKSPACE' AND (q.execution_id=ANY($2::uuid[]) OR $3>0)`, workerID, activeIDs, capacity.AvailableSlots); err != nil {
			return nil, err
		}
	}
	if job, err := scanUnnotifiedCapacityFailure(ctx, tx, workerID); err == nil && job != nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return job, nil
	} else if err != nil && !errors.Is(err, application.ErrExecutorRequestNotFound) {
		return nil, err
	}
	var jobID, requestID uuid.UUID
	var leaseUntil time.Time
	err = tx.QueryRow(ctx, `WITH candidate AS (
		SELECT j.id FROM executor_jobs j JOIN executor_requests q ON q.id=j.request_id
		JOIN executor_workers w ON w.id=j.worker_id JOIN projects p ON p.id=q.project_id
		JOIN tasks t ON t.id=q.task_id JOIN task_executions e ON e.id=q.execution_id AND e.task_id=t.id
		JOIN project_tool_settings s ON s.project_id=q.project_id AND s.tool_name=q.tool_name
		JOIN dashboard_users u ON u.id=q.decided_by
		LEFT JOIN prelo_app.approval_requests a ON a.id=q.approval_id
		WHERE j.worker_id=$1 AND j.status='READY' AND j.attempt=0
		AND q.status='APPROVED' AND q.start_before>now() AND q.worker_id=w.id
		AND q.image_digest=w.image_digest AND q.project_id=w.project_id AND w.enabled AND p.deleted_at IS NULL
		AND t.project_id=q.project_id AND t.status NOT IN ('COMPLETED','FAILED') AND e.status NOT IN ('COMPLETED','FAILED')
		AND s.enabled AND s.version=q.policy_version AND u.activated_at IS NOT NULL
		AND (q.approval_id IS NULL OR a.status='APPROVED')
		AND (u.role='ADMIN' OR EXISTS(SELECT 1 FROM project_members m WHERE m.project_id=q.project_id AND m.dashboard_user_id=u.id AND m.role='PROJECT_ADMIN'))
		AND ((q.operation='START_WORKSPACE' AND (q.execution_id=ANY($3::uuid[]) OR $4 > (
			SELECT count(*) FROM executor_jobs running JOIN executor_requests rq ON rq.id=running.request_id
			WHERE running.worker_id=$1 AND running.status IN ('CLAIMED','RUNNING') AND rq.operation='START_WORKSPACE'
			AND NOT (rq.execution_id=ANY($3::uuid[]))))) OR (q.operation<>'START_WORKSPACE' AND q.execution_id=ANY($3::uuid[])))
		ORDER BY j.created_at LIMIT 1 FOR UPDATE OF j SKIP LOCKED)
		UPDATE executor_jobs j SET status='CLAIMED',lease_id=$2,lease_expires_at=now()+interval '120 seconds',
		attempt=attempt+1,updated_at=now() FROM candidate c WHERE j.id=c.id
		RETURNING j.id,j.request_id,j.lease_expires_at`, workerID, leaseID, activeIDs, capacity.AvailableSlots).Scan(&jobID, &requestID, &leaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	q, err := scanExecutorRequest(tx.QueryRow(ctx, `SELECT `+executorRequestColumns+` FROM executor_requests WHERE id=$1`, requestID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &application.ExecutorJob{ID: jobID, Request: q, Status: "CLAIMED", LeaseID: leaseID, LeaseUntil: leaseUntil}, nil
}

func scanUnnotifiedCapacityFailure(ctx context.Context, tx pgx.Tx, workerID uuid.UUID) (*application.ExecutorJob, error) {
	var jobID, requestID uuid.UUID
	var status string
	var result []byte
	err := tx.QueryRow(ctx, `SELECT j.id,j.request_id,j.status,j.result_json FROM executor_jobs j
		JOIN executor_requests q ON q.id=j.request_id WHERE j.worker_id=$1 AND j.status IN ('UNSUPPORTED_CAPACITY','FAILED','CANCELLED')
		AND j.task_notified_at IS NULL AND q.tool_call_id IS NOT NULL ORDER BY j.updated_at LIMIT 1 FOR UPDATE OF j SKIP LOCKED`, workerID).
		Scan(&jobID, &requestID, &status, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, application.ErrExecutorRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	request, err := scanExecutorRequest(tx.QueryRow(ctx, `SELECT `+executorRequestColumns+` FROM executor_requests WHERE id=$1`, requestID))
	if err != nil {
		return nil, err
	}
	return &application.ExecutorJob{ID: jobID, Request: request, Status: status, Result: result}, nil
}
func (r *ExecutorRequestRepository) Current(ctx context.Context, workerID, jobID uuid.UUID) (application.ExecutorJob, error) {
	var requestID, leaseID uuid.UUID
	var status string
	var leaseUntil time.Time
	var sequence int
	err := r.pool.QueryRow(ctx, `SELECT j.request_id,j.status,j.lease_id,j.lease_expires_at,j.result_sequence
		FROM executor_jobs j JOIN executor_requests q ON q.id=j.request_id
		JOIN executor_workers w ON w.id=j.worker_id JOIN projects p ON p.id=q.project_id
		JOIN tasks t ON t.id=q.task_id JOIN task_executions e ON e.id=q.execution_id AND e.task_id=t.id
		JOIN project_tool_settings s ON s.project_id=q.project_id AND s.tool_name=q.tool_name
		JOIN dashboard_users u ON u.id=q.decided_by
		LEFT JOIN prelo_app.approval_requests a ON a.id=q.approval_id
		WHERE j.id=$1 AND j.worker_id=$2 AND j.status IN ('CLAIMED','RUNNING') AND j.lease_expires_at>now()
		AND q.status='APPROVED' AND (j.status='RUNNING' OR q.start_before>now()) AND q.worker_id=w.id AND q.image_digest=w.image_digest
		AND q.project_id=w.project_id AND w.enabled AND p.deleted_at IS NULL
		AND t.project_id=q.project_id AND t.status NOT IN ('COMPLETED','FAILED') AND e.status NOT IN ('COMPLETED','FAILED')
		AND s.enabled AND s.version=q.policy_version AND u.activated_at IS NOT NULL
		AND (q.approval_id IS NULL OR a.status='APPROVED')
		AND (u.role='ADMIN' OR EXISTS(SELECT 1 FROM project_members m WHERE m.project_id=q.project_id AND m.dashboard_user_id=u.id AND m.role='PROJECT_ADMIN'))`, jobID, workerID).Scan(&requestID, &status, &leaseID, &leaseUntil, &sequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ExecutorJob{}, application.ErrExecutorUnauthorized
	}
	if err != nil {
		return application.ExecutorJob{}, err
	}
	q, err := r.Find(ctx, requestID)
	if err != nil {
		return application.ExecutorJob{}, err
	}
	return application.ExecutorJob{ID: jobID, Request: q, Status: status, LeaseID: leaseID, LeaseUntil: leaseUntil, Sequence: sequence}, nil
}
func (r *ExecutorRequestRepository) Report(ctx context.Context, workerID, jobID, leaseID uuid.UUID, sequence int, status string, result json.RawMessage) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var currentStatus string
	var currentSequence int
	var currentResult []byte
	var operation string
	var requestID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT j.status,j.result_sequence,j.result_json,q.operation,q.id FROM executor_jobs j
		JOIN executor_requests q ON q.id=j.request_id
		JOIN executor_workers w ON w.id=j.worker_id JOIN projects p ON p.id=q.project_id
		JOIN project_tool_settings s ON s.project_id=q.project_id AND s.tool_name=q.tool_name
		JOIN dashboard_users u ON u.id=q.decided_by
		LEFT JOIN prelo_app.approval_requests a ON a.id=q.approval_id
		WHERE j.id=$1 AND j.worker_id=$2 AND j.lease_id=$3 AND j.lease_expires_at>now()
		AND q.status='APPROVED' AND w.enabled AND w.project_id=q.project_id AND w.image_digest=q.image_digest
		AND (q.approval_id IS NULL OR a.status='APPROVED')
		AND p.deleted_at IS NULL AND s.enabled AND s.version=q.policy_version AND u.activated_at IS NOT NULL
		AND (u.role='ADMIN' OR EXISTS(SELECT 1 FROM project_members m WHERE m.project_id=q.project_id AND m.dashboard_user_id=u.id AND m.role='PROJECT_ADMIN'))
		FOR UPDATE OF j`, jobID, workerID, leaseID).Scan(&currentStatus, &currentSequence, &currentResult, &operation, &requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrExecutorUnauthorized
	}
	if err != nil {
		return err
	}
	if sequence == currentSequence && status == currentStatus && equalJSON(result, currentResult) {
		return nil
	}
	if sequence <= currentSequence || (currentStatus != "CLAIMED" && currentStatus != "RUNNING") {
		return application.ErrExecutorConflict
	}
	if (currentStatus == "CLAIMED" && status != "RUNNING" && status != "FAILED" && status != "CANCELLED") ||
		(currentStatus == "RUNNING" && status != "SUCCEEDED" && status != "FAILED" && status != "CANCELLED") {
		return application.ErrExecutorConflict
	}
	if status == "SUCCEEDED" && operation == "CREATE" {
		var published bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project_files WHERE executor_request_id=$1 AND deleted_at IS NULL)`, requestID).Scan(&published); err != nil {
			return err
		}
		if !published {
			return application.ErrExecutorConflict
		}
	}
	_, err = tx.Exec(ctx, `UPDATE executor_jobs SET status=$2,result_sequence=$3,result_json=$4,updated_at=now(),
		lease_expires_at=CASE WHEN $2='RUNNING' THEN now()+interval '120 seconds' ELSE lease_expires_at END
		WHERE id=$1`, jobID, status, sequence, result)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func equalJSON(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	leftCanonical, _ := json.Marshal(left)
	rightCanonical, _ := json.Marshal(right)
	return string(leftCanonical) == string(rightCanonical)
}
