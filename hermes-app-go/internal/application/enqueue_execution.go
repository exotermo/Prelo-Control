package application

import (
	"context"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

// JobPublisher is the best-effort Redis dispatch signal (see ADR-013) — a failure here never
// fails the enqueue itself, since the durable Postgres rows are already committed by the time
// it's called.
type JobPublisher interface {
	Publish(ctx context.Context, jobID string)
}

// EnqueueExecutionUseCase is the API-facing half of etapa 6.5's async flow: it does the
// minimum work needed to durably record "this task should run" and return fast — the actual
// orchestration (agent/context resolution, the Gateway call) happens later, in
// ProcessJobUseCase, run by a worker consuming the queue.
type EnqueueExecutionUseCase struct {
	tasks      TaskRepository
	executions ExecutionRepository
	jobs       ExecutionJobRepository
	publisher  JobPublisher
}

func NewEnqueueExecutionUseCase(tasks TaskRepository, executions ExecutionRepository, jobs ExecutionJobRepository, publisher JobPublisher) *EnqueueExecutionUseCase {
	return &EnqueueExecutionUseCase{tasks: tasks, executions: executions, jobs: jobs, publisher: publisher}
}

func (uc *EnqueueExecutionUseCase) Enqueue(ctx context.Context, taskID domain.TaskID) (domain.Execution, error) {
	task, err := uc.tasks.FindByID(ctx, taskID)
	if err != nil {
		return domain.Execution{}, err
	}

	// Queued() is only allowed from CREATED — a task already QUEUED (or beyond) is rejected
	// here, which is what makes a second concurrent POST .../execute on the same task
	// naturally rejected rather than silently creating a duplicate job.
	queuedTask, err := task.Queued()
	if err != nil {
		return domain.Execution{}, err
	}
	if _, err := uc.tasks.Update(ctx, queuedTask); err != nil {
		return domain.Execution{}, err
	}

	execution := domain.NewPendingExecution(taskID, task.AgentID)
	if err := uc.executions.Insert(ctx, execution); err != nil {
		return domain.Execution{}, err
	}

	job := domain.NewExecutionJob(taskID, execution.ID)
	if err := uc.jobs.Insert(ctx, job); err != nil {
		return domain.Execution{}, err
	}

	uc.publisher.Publish(ctx, execution.ID.String())
	return execution, nil
}
