package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

type TaskRepository struct {
	pool *pgxpool.Pool
}

func NewTaskRepository(pool *pgxpool.Pool) *TaskRepository {
	return &TaskRepository{pool: pool}
}

func (r *TaskRepository) Insert(ctx context.Context, task domain.Task) error {
	var parentTaskID *uuid.UUID
	if task.ParentTaskID != nil {
		parentTaskID = &task.ParentTaskID.Value
	}
	var tenantID any
	if task.TenantID != "" {
		tenantID = task.TenantID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tasks (id, tenant_id, description, status, created_at, agent_id, task_version, parent_task_id, depth)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		task.ID.Value, tenantID, task.Description, string(task.Status), task.CreatedAt, task.AgentID.Value, task.Version, parentTaskID, task.Depth)
	return err
}

func (r *TaskRepository) FindByID(ctx context.Context, id domain.TaskID) (domain.Task, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, description, status, created_at, agent_id, task_version, parent_task_id, depth
		  FROM tasks WHERE id = $1`, id.Value)
	return scanTask(row)
}

func (r *TaskRepository) FindByIDForTenant(ctx context.Context, id domain.TaskID, tenantID string) (domain.Task, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, description, status, created_at, agent_id, task_version, parent_task_id, depth
		  FROM tasks WHERE id = $1 AND tenant_id = $2`, id.Value, tenantID)
	return scanTask(row)
}

// FindChildren lists every Task delegated from parentID, newest first — the raw material for
// the observability API's task tree (Fase D) and for anything that needs to know "what has this
// task spawned".
func (r *TaskRepository) FindChildren(ctx context.Context, parentID domain.TaskID) ([]domain.Task, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, description, status, created_at, agent_id, task_version, parent_task_id, depth
		  FROM tasks WHERE parent_task_id = $1 ORDER BY created_at`, parentID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var children []domain.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		children = append(children, task)
	}
	return children, rows.Err()
}

// ListRoots lists top-level Tasks (parent_task_id IS NULL — a delegated sub-task only ever
// shows up via its parent's tree, never in this list on its own), newest first, capped at
// limit. Backs the dashboard's task list (Fase E) — there was no "list tasks" read path before
// it, only lookup-by-id.
func (r *TaskRepository) ListRoots(ctx context.Context, limit int) ([]domain.Task, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, description, status, created_at, agent_id, task_version, parent_task_id, depth
		  FROM tasks WHERE parent_task_id IS NULL ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []domain.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (r *TaskRepository) ListRootsForTenant(ctx context.Context, tenantID string, limit int) ([]domain.Task, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, description, status, created_at, agent_id, task_version, parent_task_id, depth
		  FROM tasks WHERE parent_task_id IS NULL AND tenant_id = $1 ORDER BY created_at DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []domain.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func scanTask(row pgx.Row) (domain.Task, error) {
	var t domain.Task
	var status, agentID string
	var tenantID *uuid.UUID
	var parentTaskID *uuid.UUID
	if err := row.Scan(&t.ID.Value, &tenantID, &t.Description, &status, &t.CreatedAt, &agentID, &t.Version, &parentTaskID, &t.Depth); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Task{}, application.ErrTaskNotFound
		}
		return domain.Task{}, err
	}
	t.Status = domain.TaskStatus(status)
	if tenantID != nil {
		t.TenantID = tenantID.String()
	}
	t.AgentID = domain.AgentID{Value: agentID}
	if parentTaskID != nil {
		id := domain.TaskID{Value: *parentTaskID}
		t.ParentTaskID = &id
	}
	return t, nil
}

// Update performs a version-checked write (`WHERE id=$1 AND task_version=$2`). The caller is
// expected to have already applied a guarded domain transition (Task.Running(), etc.) to
// `task` in-process; this method does not re-check status server-side, matching the Java
// use cases where the in-memory guard runs before the DB write.
func (r *TaskRepository) Update(ctx context.Context, task domain.Task) (domain.Task, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE tasks
		   SET description = $2, status = $3, agent_id = $4, task_version = task_version + 1
		 WHERE id = $1 AND task_version = $5
		 RETURNING task_version`,
		task.ID.Value, task.Description, string(task.Status), task.AgentID.Value, task.Version)

	var newVersion int64
	if err := row.Scan(&newVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Task{}, application.ErrOptimisticLock
		}
		return domain.Task{}, err
	}
	return task.WithVersion(newVersion), nil
}
