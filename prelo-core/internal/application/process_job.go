package application

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/gateway"
)

// defaultLeaseDuration must cover the worst case of one job's whole body, not just one Gateway
// call: since Fase B, that can be up to maxLLMCalls sequential Gateway calls (each up to the
// Gateway's own ~10s timeout) plus tool executions, not the single call etapa 6.5 originally
// sized this for. MarkRunning renews the lease to exactly this duration when a job actually
// starts running, so this only needs to cover one full loop, not the time spent queued.
const defaultLeaseDuration = 6 * time.Minute

// ProcessJobUseCase is the worker's job body (etapa 6.5): given an executionID whose
// Execution+ExecutionJob rows were already created by EnqueueExecutionUseCase, it claims the
// job, moves the Task to RUNNING, resolves context, calls the Gateway, and finalizes both the
// Execution and the job. It mirrors ExecuteTaskUseCase.java's ordering and its distinction
// between failures preserved as a terminal FAILED Execution (context preparation, Gateway
// call — both happen after the Execution already exists and must stay attached to it for
// audit) versus earlier failures (task not found, invalid transition, unknown agent), which
// here also resolve to a terminal FAILED Execution+job rather than propagating as raw errors,
// since there is no synchronous HTTP caller left to receive them.
type ProcessJobUseCase struct {
	tasks       TaskRepository
	executions  ExecutionRepository
	jobs        ExecutionJobRepository
	agents      AgentRegistry
	resolver    *ContextResolver
	snapshots   ContextSnapshotRepository
	loop        *RunAgentLoopUseCase
	turns       ExecutionTurnRepository
	suspensions ExecutionSuspensionRepository
	workerID    string
	events      EventPublisher
}

// loop owns the actual Gateway calls now (Fase B) — ProcessJobUseCase only owns claiming the
// job, the Task/Execution first-entry-vs-resume transitions, and finalizing whatever the loop
// returns. turns/suspensions (Fase C) are only used by resolveParentSubtaskSuspension, the hook
// that wakes a delegating parent back up once its child Task finishes. Construct
// RunAgentLoopUseCase with its own LanguageModelGateway separately.
func NewProcessJobUseCase(tasks TaskRepository, executions ExecutionRepository, jobs ExecutionJobRepository, agents AgentRegistry, resolver *ContextResolver, snapshots ContextSnapshotRepository, loop *RunAgentLoopUseCase, turns ExecutionTurnRepository, suspensions ExecutionSuspensionRepository, workerID string) *ProcessJobUseCase {
	return &ProcessJobUseCase{
		tasks: tasks, executions: executions, jobs: jobs, agents: agents,
		resolver: resolver, snapshots: snapshots, loop: loop, turns: turns, suspensions: suspensions, workerID: workerID,
		events: NoopEventPublisher{},
	}
}

// SetEventPublisher wires Fase I's webhook fan-out; without it, terminal transitions publish
// nowhere (NoopEventPublisher).
func (uc *ProcessJobUseCase) SetEventPublisher(events EventPublisher) { uc.events = events }

func taskEventData(task domain.Task, execution domain.Execution) map[string]any {
	data := map[string]any{
		"taskId": task.ID.String(), "executionId": execution.ID.String(), "agentId": task.AgentID.String(),
		"description": task.Description, "status": string(task.Status),
	}
	if execution.Result != nil {
		result := *execution.Result
		if len(result) > 8000 {
			result = result[:8000]
		}
		data["result"] = result
	}
	if execution.Error != nil {
		data["error"] = *execution.Error
	}
	return data
}

// ProcessExecution claims and processes exactly one job. A lost claim (another worker, or a
// concurrent duplicate wake, already has it) is not an error — it returns the Execution as it
// currently stands and a nil error, so the worker loop simply moves on.
func (uc *ProcessJobUseCase) ProcessExecution(ctx context.Context, executionID domain.ExecutionID) (domain.Execution, error) {
	claimed, err := uc.jobs.Claim(ctx, executionID, uc.workerID, defaultLeaseDuration)
	if err != nil {
		if errors.Is(err, ErrJobClaimLost) {
			return uc.executions.FindByID(ctx, executionID)
		}
		return domain.Execution{}, err
	}
	if err := uc.jobs.MarkRunning(ctx, executionID, defaultLeaseDuration); err != nil {
		if errors.Is(err, ErrJobClaimLost) {
			// Someone else already decided this worker lost the job (e.g. the sweeper reclaimed
			// it as orphaned right in this narrow window) — same "not an error, just move on"
			// handling as a lost Claim.
			return uc.executions.FindByID(ctx, executionID)
		}
		return domain.Execution{}, err
	}

	execution, err := uc.executions.FindByID(ctx, executionID)
	if err != nil {
		return domain.Execution{}, err
	}

	// Idempotency guard: if the Execution already reached a terminal state — e.g. this job
	// was redelivered after a crash between "Gateway responded" and "job marked DONE" — don't
	// call the Gateway again. The execution id doubles as the Gateway's requestId, so at most
	// one real call could ever have happened for it; we just reconcile the job to match.
	if execution.Status == domain.ExecutionCompleted {
		_ = uc.jobs.MarkDone(ctx, executionID)
		return execution, nil
	}
	if execution.Status == domain.ExecutionFailed {
		_ = uc.jobs.MarkFailed(ctx, executionID, errorMessageOrEmpty(execution.Error))
		return execution, nil
	}

	task, err := uc.tasks.FindByID(ctx, claimed.TaskID)
	if err != nil {
		return uc.terminalFailure(ctx, execution, err)
	}

	// A job re-claimed after AWAITING_RESUME (approval decided, or a sub-task completed) finds
	// the Task already RUNNING — Task.Running() only allows CREATED/QUEUED, so re-calling it
	// here would fail a resume every time. First entry (fresh PENDING job) is the only case
	// that actually transitions the Task.
	running := task
	if task.Status != domain.TaskRunning {
		runningStatus, err := task.Running()
		if err != nil {
			return uc.terminalFailure(ctx, execution, err)
		}
		// Capturing the store's return value matters: it carries the row version this write
		// just landed at, and this write is the only place Task moves into RUNNING for this job.
		running, err = uc.tasks.Update(ctx, runningStatus)
		if err != nil {
			return uc.terminalFailure(ctx, execution, err)
		}
	}

	agent, err := uc.agents.FindRequired(running.AgentID)
	if err != nil {
		return uc.terminalFailure(ctx, execution, err)
	}

	// Same first-entry-vs-resume split for the Execution/snapshot: on resume, execution.Status
	// is already RUNNING and execution.ContextSnapshotID already points at the snapshot the
	// loop's first turn used — reusing it is what keeps "what context was this run given"
	// stable across suspend/resume instead of silently re-resolving (and possibly changing)
	// context mid-execution.
	var snapshot domain.ContextSnapshot
	if execution.Status != domain.ExecutionRunning {
		execution, err = uc.executions.Update(ctx, execution.Running().WithAgentVersion(agent.Version))
		if err != nil {
			return domain.Execution{}, err
		}

		snapshot, err = uc.resolver.Resolve(ctx, running)
		if err == nil {
			snapshot, err = uc.snapshots.Save(ctx, snapshot)
		}
		if err != nil {
			return uc.failContextPreparation(ctx, running, execution, err)
		}
		// Persisted immediately, before the loop runs: if it fails, the snapshot that was
		// actually used stays associated with the (now FAILED) execution for audit/reproduction.
		execution, err = uc.executions.Update(ctx, execution.WithContextSnapshotID(snapshot.ID))
		if err != nil {
			return domain.Execution{}, err
		}
	} else {
		snapshot, err = uc.snapshots.FindByID(ctx, *execution.ContextSnapshotID)
		if err != nil {
			return domain.Execution{}, err
		}
	}

	result, err := uc.loop.Run(ctx, running, agent, snapshot, execution)
	if err != nil {
		return domain.Execution{}, err
	}

	switch result.Outcome {
	case LoopSuspended:
		// The loop already moved the job to AWAITING_RESUME itself — nothing left to do here,
		// and nothing about Task/Execution/job state should be touched further.
		return execution, nil

	case LoopFailed:
		return uc.failExecution(ctx, running, execution.WithRequestID(result.Request), sanitizedFailureMessage(result.Err))

	default: // LoopCompleted
		completedTask, terr := running.Completed()
		if terr != nil {
			return domain.Execution{}, terr
		}
		if _, err := uc.tasks.Update(ctx, completedTask); err != nil {
			return domain.Execution{}, err
		}
		completed := execution.Completed(result.Content, result.Request, result.Model, result.Provider)
		updated, err := uc.executions.Update(ctx, completed)
		if err != nil {
			return domain.Execution{}, err
		}
		if err := uc.jobs.MarkDone(ctx, executionID); err != nil {
			return domain.Execution{}, err
		}
		uc.events.Publish(ctx, completedTask.ProjectID, domain.EventTaskCompleted, taskEventData(completedTask, updated))
		if err := uc.resolveParentSubtaskSuspension(ctx, updated.TaskID, updated); err != nil {
			return domain.Execution{}, err
		}
		return updated, nil
	}
}

// resolveParentSubtaskSuspension is Fase C's completion hook: run after every terminal
// Execution (completed or failed), it checks whether taskID is a child some OTHER execution
// delegated to and is still waiting on (domain.SuspensionSubtask, ResumeKey=taskID) — if so, it
// completes that parent's open ledger turn with this child's outcome, resolves the suspension,
// and resumes the parent's job. A no-op for the overwhelming majority of executions, which were
// never anyone's delegated sub-task.
func (uc *ProcessJobUseCase) resolveParentSubtaskSuspension(ctx context.Context, taskID domain.TaskID, execution domain.Execution) error {
	suspension, found, err := uc.suspensions.FindActiveByResumeKey(ctx, domain.SuspensionSubtask, taskID.String())
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	outcome := "subtask produced no result"
	switch {
	case execution.Status == domain.ExecutionCompleted && execution.Result != nil:
		outcome = *execution.Result
	case execution.Status == domain.ExecutionFailed:
		outcome = "error: " + errorMessageOrEmpty(execution.Error)
	}

	turns, err := uc.turns.ListByExecution(ctx, suspension.ExecutionID)
	if err != nil {
		return err
	}
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].CompletedAt == nil {
			if _, err := uc.turns.Update(ctx, turns[i].Completed(outcome)); err != nil {
				return err
			}
			break
		}
	}

	// A concurrent resolver already handled this suspension — back off instead of resuming
	// (jobs.Resume) a second time for the same execution.
	if err := uc.suspensions.Resolve(ctx, suspension.ID); err != nil {
		if errors.Is(err, ErrExecutionSuspensionAlreadyResolved) {
			return nil
		}
		return err
	}
	log.Printf("agent-loop: execution=%s resumed by completed subtask childTask=%s childStatus=%s", suspension.ExecutionID, taskID, execution.Status)
	return uc.jobs.Resume(ctx, suspension.ExecutionID)
}

// terminalFailure handles errors surfacing before the Execution has a Task to guard a Failed()
// transition through (task not found, invalid transition, unknown agent) — there is no
// synchronous HTTP caller left to hand a 404/409/400 to, so these become a terminal FAILED
// Execution+job instead, same as a context-preparation or Gateway failure would.
func (uc *ProcessJobUseCase) terminalFailure(ctx context.Context, execution domain.Execution, cause error) (domain.Execution, error) {
	updated, err := uc.executions.Update(ctx, execution.Failed(cause.Error()))
	if err != nil {
		return domain.Execution{}, err
	}
	if err := uc.jobs.MarkFailed(ctx, execution.ID, cause.Error()); err != nil {
		return domain.Execution{}, err
	}
	if err := uc.resolveParentSubtaskSuspension(ctx, updated.TaskID, updated); err != nil {
		return domain.Execution{}, err
	}
	return updated, nil
}

func (uc *ProcessJobUseCase) failContextPreparation(ctx context.Context, running domain.Task, execution domain.Execution, cause error) (domain.Execution, error) {
	return uc.failExecution(ctx, running, execution, fmt.Sprintf("context preparation failed: %s", cause.Error()))
}

func (uc *ProcessJobUseCase) failExecution(ctx context.Context, running domain.Task, execution domain.Execution, message string) (domain.Execution, error) {
	failedTask, err := running.Failed()
	if err != nil {
		return domain.Execution{}, err
	}
	if _, err := uc.tasks.Update(ctx, failedTask); err != nil {
		return domain.Execution{}, err
	}
	updated, err := uc.executions.Update(ctx, execution.Failed(message))
	if err != nil {
		return domain.Execution{}, err
	}
	// Not a retry/backoff decision here (that's the sweeper's job, etapa G11) — this call
	// simply marks the job terminal; the sweeper is what would later move a *different* kind
	// of failure (an orphaned lease) into RETRY.
	if err := uc.jobs.MarkFailed(ctx, execution.ID, message); err != nil {
		return domain.Execution{}, err
	}
	uc.events.Publish(ctx, failedTask.ProjectID, domain.EventTaskFailed, taskEventData(failedTask, updated))
	if err := uc.resolveParentSubtaskSuspension(ctx, updated.TaskID, updated); err != nil {
		return domain.Execution{}, err
	}
	return updated, nil
}

// sanitizedFailureMessage is what actually ends up in Execution.Error — served back
// unauthenticated via GET /tasks/{id}/executions/{id} (prelo-core has no auth today), so a raw
// *gateway.CallError.Error() (which wraps the real transport error — a DNS failure, an
// internal hostname, etc.) must never land here as-is. api/errors.go already does the
// equivalent sanitization for the synchronous /chat path; this is the same rule applied
// to the async loop's own failure path.
func sanitizedFailureMessage(err error) string {
	var callErr *gateway.CallError
	if errors.As(err, &callErr) {
		return callErr.Message
	}
	return err.Error()
}

func errorMessageOrEmpty(msg *string) string {
	if msg == nil {
		return ""
	}
	return *msg
}
