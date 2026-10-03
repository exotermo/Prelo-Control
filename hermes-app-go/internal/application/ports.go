package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/gateway"
)

// ErrOptimisticLock is returned by a repository's versioned Update when the row's current
// version no longer matches the version the caller last read — either because another writer
// won the race, or (for Execution, which has no DB-level status guard) any other lost update.
var ErrOptimisticLock = errors.New("optimistic lock conflict")

var ErrTaskNotFound = errors.New("task not found")
var ErrExecutionNotFound = errors.New("execution not found")
var ErrContextSnapshotNotFound = errors.New("context snapshot not found")

// TaskRepository mirrors TaskRepository.java. Update performs a version-checked write
// (`WHERE id=$1 AND task_version=$2`) and returns ErrOptimisticLock on a lost race — it does
// NOT re-check status server-side; the guarded transition (Task.Running()/Completed()/...)
// must already have been applied to the Task value in-process before calling Update, exactly
// like the Java use cases do.
type TaskRepository interface {
	Insert(ctx context.Context, task domain.Task) error
	FindByID(ctx context.Context, id domain.TaskID) (domain.Task, error)
	Update(ctx context.Context, task domain.Task) (domain.Task, error)
	// FindChildren backs the Fase C delegation tree (delegate_to_agent) and the Fase D
	// observability API — every Task whose ParentTaskID is parentID, oldest first.
	FindChildren(ctx context.Context, parentID domain.TaskID) ([]domain.Task, error)
	// ListRoots backs the Fase E dashboard's task list — top-level tasks only (a delegated
	// sub-task shows up via its parent's tree, not here), newest first, capped at limit.
	ListRoots(ctx context.Context, limit int) ([]domain.Task, error)
	// ListActiveRoots backs the Fase P Pipeline page — top-level tasks whose Status is not yet
	// terminal (CREATED/QUEUED/RUNNING), newest first, capped at limit.
	ListActiveRoots(ctx context.Context, limit int) ([]domain.Task, error)
	// ListRootsByProject/ListActiveRootsByProject back the Fase W project-scoped Tasks/Pipeline
	// pages — nil projectID means the "unassigned" bucket (project_id IS NULL).
	ListRootsByProject(ctx context.Context, projectID *uuid.UUID, limit int) ([]domain.Task, error)
	ListActiveRootsByProject(ctx context.Context, projectID *uuid.UUID, limit int) ([]domain.Task, error)
}

type ExecutionRepository interface {
	Insert(ctx context.Context, execution domain.Execution) error
	FindByID(ctx context.Context, id domain.ExecutionID) (domain.Execution, error)
	Update(ctx context.Context, execution domain.Execution) (domain.Execution, error)
	// FindByTaskID assumes the current 1:1 Task→Execution relationship — the Fase D task tree
	// needs each node's own execution status, not just the Task row.
	FindByTaskID(ctx context.Context, taskID domain.TaskID) (domain.Execution, error)
}

// ManualContextRepository mirrors ManualContextRepository.java.
type ManualContextRepository interface {
	Save(ctx context.Context, taskID domain.TaskID, items []domain.ManualContextItem) error
	FindByTaskID(ctx context.Context, taskID domain.TaskID) ([]domain.ManualContextItem, error)
}

// ContextSnapshotRepository mirrors ContextSnapshotRepository.java. FindLatestByTaskID
// returns (ContextSnapshot{}, false, nil) when none exists yet — mirroring Optional.empty(),
// not an error.
type ContextSnapshotRepository interface {
	Save(ctx context.Context, snapshot domain.ContextSnapshot) (domain.ContextSnapshot, error)
	FindByID(ctx context.Context, id domain.ContextSnapshotID) (domain.ContextSnapshot, error)
	FindLatestByTaskID(ctx context.Context, taskID domain.TaskID) (domain.ContextSnapshot, bool, error)
}

// AgentRegistry mirrors AgentDefinitionRegistry.java — a curated, boot-validated catalog.
// FindRequired returns *domain.ErrUnknownAgent when the id isn't in the catalog.
type AgentRegistry interface {
	Find(agentID domain.AgentID) (domain.AgentDefinition, bool)
	FindRequired(agentID domain.AgentID) (domain.AgentDefinition, error)
}

// LanguageModelGateway mirrors LanguageModelGateway.java, used directly by ChatService for
// the /hermes/chat passthrough endpoint (distinct from the ProcessJobUseCase path, which goes
// through the narrower LlmClient-style port instead).
type LanguageModelGateway interface {
	Chat(ctx context.Context, req gateway.ChatRequest, requestID string) (gateway.ChatResponse, error)
}

// HermesExecutionRecord mirrors the HermesExecution JPA entity (table hermes_llm_executions,
// Flyway V1) — a lightweight audit row for every /hermes/chat call, success or failure. It
// carries no domain invariants, so it lives here as a plain struct rather than in the domain
// package.
type HermesExecutionRecord struct {
	RequestID      string
	TaskID         string
	AgentID        string
	RequestedModel string
	Provider       *string
	DurationMs     *int64
	Status         string
}

type HermesExecutionRepository interface {
	Save(ctx context.Context, record HermesExecutionRecord) error
}

var ErrJobNotFound = errors.New("execution job not found")

// ErrJobClaimLost is returned by Claim when the job is no longer PENDING/RETRY by the time
// the UPDATE runs — either another worker already claimed it, or the sweeper hasn't yet made
// it claimable again. Unlike Task/Execution's ErrOptimisticLock, this isn't a version
// mismatch against a value the caller already held; it's the same "belt and suspenders"
// status-guarded claim described in ADR-013.
var ErrJobClaimLost = errors.New("execution job claim lost")

// ToolExecutor pairs a curated domain.ToolDefinition with the actual code that runs when a
// call is allowed — the domain package itself stays free of I/O, same split as
// AgentDefinition vs. the Gateway call that actually talks to an LLM.
// Execute receives the calling Execution (not just its id) because delegate_to_agent (Fase C)
// needs the caller's TaskID to look up the parent Task — every other tool today just ignores
// it, but the port carries it for any tool, not only that one.
type ToolExecutor interface {
	Definition() domain.ToolDefinition
	Execute(ctx context.Context, execution domain.Execution, argsJSON string) (string, error)
}

// ToolRegistry mirrors AgentRegistry: a curated, boot-validated catalog, never built from
// request input.
type ToolRegistry interface {
	Find(name string) (ToolExecutor, bool)
	List() []domain.ToolDefinition
}

// PermissionPolicy decides, for one agent+tool pair, whether a call may run immediately, must
// be denied outright, or must wait on a human's explicit approval. It never runs anything
// itself and it is never asked to authorize an agent for its own request — the caller
// (InvokeToolUseCase) is what enforces that the agent actually holds the capability before
// PermissionPolicy is even consulted (AGENTS.md: "Um agente não amplia seus próprios scopes
// nem decide sua autorização").
type PermissionPolicy interface {
	Evaluate(agent domain.AgentDefinition, tool domain.ToolDefinition) domain.PermissionDecision
}

var ErrToolNotFound = errors.New("tool not found")
var ErrToolCallNotFound = errors.New("tool call not found")
var ErrApprovalNotFound = errors.New("approval request not found")
var ErrApprovalNotPending = errors.New("approval request is not pending")

type ToolCallRepository interface {
	Insert(ctx context.Context, call domain.ToolCall) error
	FindByID(ctx context.Context, id domain.ToolCallID) (domain.ToolCall, error)
	Update(ctx context.Context, call domain.ToolCall) (domain.ToolCall, error)
}

type ApprovalRepository interface {
	Insert(ctx context.Context, approval domain.ApprovalRequest) error
	FindByID(ctx context.Context, id domain.ApprovalRequestID) (domain.ApprovalRequest, error)
	Update(ctx context.Context, approval domain.ApprovalRequest) (domain.ApprovalRequest, error)
	ListPending(ctx context.Context) ([]domain.ApprovalRequest, error)
	// ListPendingByProject backs the Fase W project-scoped Aprovações page — nil projectID
	// means the "unassigned" bucket (joins through tool_calls/tasks, see the Postgres impl).
	ListPendingByProject(ctx context.Context, projectID *uuid.UUID) ([]domain.ApprovalRequest, error)
}

// ExecutionJobRepository mirrors the etapa 6.5 queue design: Insert creates a PENDING job
// alongside a fresh Execution, Claim atomically transitions it to CLAIMED for exactly one
// caller, and MarkDone/MarkFailed close it out. Retry/backoff/lease-based reclaiming is the
// sweeper's job (etapa G11), not this port.
type ExecutionJobRepository interface {
	Insert(ctx context.Context, job domain.ExecutionJob) error
	FindByID(ctx context.Context, id domain.ExecutionID) (domain.ExecutionJob, error)
	Claim(ctx context.Context, id domain.ExecutionID, workerID string, leaseDuration time.Duration) (domain.ExecutionJob, error)
	// MarkRunning also renews the lease (leaseDuration from now) and only succeeds while the
	// job is still CLAIMED — see the Postgres implementation's doc comment for why a long
	// multi-turn loop (Fase B) needs this, not just the status flip the single-Gateway-call
	// design (etapa 6.5) originally had. MarkDone/MarkFailed only succeed while RUNNING. All
	// three return ErrJobClaimLost if the guard fails — the caller no longer holds this job and
	// must stop, matching Claim's own ErrJobClaimLost convention.
	MarkRunning(ctx context.Context, id domain.ExecutionID, leaseDuration time.Duration) error
	MarkDone(ctx context.Context, id domain.ExecutionID) error
	MarkFailed(ctx context.Context, id domain.ExecutionID, message string) error

	// Suspend moves a job (currently held by the calling worker, status RUNNING) to
	// AWAITING_RESUME — no lease, deliberately excluded from RequeueOrphaned's
	// status IN ('CLAIMED','RUNNING') sweep (see the Postgres implementation's own note on
	// why that exclusion needs no extra code: it's just not in that IN list). Resume is the
	// only path back to PENDING, and only whoever resolves the matching ExecutionSuspension
	// (DecideApprovalUseCase, or a completed sub-task's hook) may call it.
	Suspend(ctx context.Context, id domain.ExecutionID) error
	Resume(ctx context.Context, id domain.ExecutionID) error

	// ListClaimable and RequeueOrphaned back the sweeper (etapa G11) — see their Postgres
	// implementation for the exact semantics.
	ListClaimable(ctx context.Context, limit int) ([]domain.ExecutionID, error)
	RequeueOrphaned(ctx context.Context) ([]domain.ExecutionID, error)
}

var ErrExecutionTurnNotFound = errors.New("execution turn not found")
var ErrExecutionSuspensionNotFound = errors.New("execution suspension not found")
var ErrExecutionSuspensionAlreadyResolved = errors.New("execution suspension already resolved")

// ExecutionTurnRepository is the append-only ledger backing the agent loop (etapa "Fase A").
// Insert is expected to be idempotent-safe by construction: callers always derive RequestID
// via domain.TurnRequestID before inserting, so a retried turn for a job the sweeper
// reprocessed collides on that natural key instead of silently duplicating a side effect —
// see the Postgres implementation for the exact conflict handling.
type ExecutionTurnRepository interface {
	Insert(ctx context.Context, turn domain.ExecutionTurn) (domain.ExecutionTurn, error)
	Update(ctx context.Context, turn domain.ExecutionTurn) (domain.ExecutionTurn, error)
	ListByExecution(ctx context.Context, executionID domain.ExecutionID) ([]domain.ExecutionTurn, error)
	// FindByRequestID lets a resumed loop check "did this exact turn already run?" before
	// calling the Gateway again — the idempotency check itself, not just the audit trail.
	FindByRequestID(ctx context.Context, requestID string) (domain.ExecutionTurn, bool, error)
}

type ExecutionSuspensionRepository interface {
	Insert(ctx context.Context, suspension domain.ExecutionSuspension) error
	// FindActiveByResumeKey is how a resolver (approval decided, sub-task completed) finds
	// which suspended execution, if any, is waiting on it — ResolvedAt IS NULL.
	FindActiveByResumeKey(ctx context.Context, reason domain.SuspensionReason, resumeKey string) (domain.ExecutionSuspension, bool, error)
	// FindActiveByExecutionID is the reverse direction — the Fase P Pipeline page's "is this
	// execution suspended right now, and why?" lookup.
	FindActiveByExecutionID(ctx context.Context, executionID domain.ExecutionID) (domain.ExecutionSuspension, bool, error)
	// Resolve is guarded to only succeed once — two concurrent resolvers that both read the
	// same suspension via FindActiveByResumeKey before either resolves it (e.g. a duplicate
	// approval-decision delivery) must not both go on to call jobs.Resume for the same
	// execution. The loser gets ErrExecutionSuspensionAlreadyResolved and must treat that as
	// "someone else already handled this", not as a hard failure.
	Resolve(ctx context.Context, id domain.ExecutionSuspensionID) error
}
