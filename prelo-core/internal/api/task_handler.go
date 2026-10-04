package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type TaskHandler struct {
	create     *application.CreateTaskUseCase
	tasks      application.TaskRepository
	executions application.ExecutionRepository
	enqueue    *application.EnqueueExecutionUseCase
}

type tenantTaskReader interface {
	FindByIDForTenant(context.Context, domain.TaskID, string) (domain.Task, error)
	ListRootsForTenant(context.Context, string, int) ([]domain.Task, error)
}

func NewTaskHandler(create *application.CreateTaskUseCase, tasks application.TaskRepository, executions application.ExecutionRepository, enqueue *application.EnqueueExecutionUseCase) *TaskHandler {
	return &TaskHandler{create: create, tasks: tasks, executions: executions, enqueue: enqueue}
}

func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid request body"})
		return
	}

	// Per-field limits (enforced by domain.NewManualContextItem below) bound each value
	// individually, but their sum can still exceed what ContextSnapshot allows for a compiled
	// prompt — checked explicitly here, before anything is persisted, matching
	// TaskController.requireWithinContextBudget.
	if len(req.Context) > domain.ContextSnapshotMaxItems {
		writeError(w, &domain.ValidationError{Message: "at most 20 manual context items are allowed"})
		return
	}
	aggregate := len(req.Description)
	for _, c := range req.Context {
		aggregate += len(c.Content)
	}
	if aggregate > domain.ContextSnapshotMaxAggregateContentLength {
		writeError(w, &domain.ValidationError{Message: "description plus context content totals over the 24000 character budget"})
		return
	}

	items := make([]domain.ManualContextItem, 0, len(req.Context))
	for _, c := range req.Context {
		item, err := domain.NewManualContextItem(c.Name, c.Content)
		if err != nil {
			writeError(w, err)
			return
		}
		items = append(items, item)
	}

	var agentID *domain.AgentID
	if req.AgentID != nil && *req.AgentID != "" {
		id, err := domain.NewAgentID(*req.AgentID)
		if err != nil {
			writeError(w, err)
			return
		}
		agentID = &id
	}

	source := domain.TaskSourceManual
	if req.Source != nil && *req.Source != "" {
		switch domain.TaskSource(*req.Source) {
		case domain.TaskSourceManual, domain.TaskSourceMessaging:
			source = domain.TaskSource(*req.Source)
		default:
			writeError(w, &domain.ValidationError{Message: "source must be MANUAL or MESSAGING"})
			return
		}
	}

	var projectID *domain.ProjectID
	if raw := projectIdentity(r.Context()); raw != nil {
		id := domain.ProjectID{Value: *raw}
		projectID = &id
	}

	var task domain.Task
	var err error
	contact := ""
	if req.ContactAddress != nil {
		contact = strings.TrimSpace(*req.ContactAddress)
	}
	if contact != "" && source != domain.TaskSourceMessaging {
		writeError(w, &domain.ValidationError{Message: "contactAddress is only accepted with source MESSAGING"})
		return
	}
	identity, isTenant := tenantIdentity(r.Context())
	if contact != "" && !isTenant {
		writeError(w, &domain.ValidationError{Message: "contactAddress is only accepted from the messaging integration"})
		return
	}
	if contact != "" {
		task, err = h.create.CreateForTenantFromContact(r.Context(), identity.TenantID, req.Description, items, agentID, projectID, contact)
	} else if isTenant {
		task, err = h.create.CreateForTenant(r.Context(), identity.TenantID, req.Description, items, agentID, source, projectID)
	} else {
		task, err = h.create.Create(r.Context(), req.Description, items, agentID, source, projectID)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, taskResponseFrom(task))
}

// List returns the most recent top-level tasks (Fase E — the dashboard's task list). Capped at
// 50; no pagination yet, matching the observability endpoints' own "read what's there, no
// query params" scope for this slice.
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	var tasks []domain.Task
	var err error
	if identity, ok := tenantIdentity(r.Context()); ok {
		if scoped, supported := h.tasks.(tenantTaskReader); supported {
			tasks, err = scoped.ListRootsForTenant(r.Context(), identity.TenantID, 50)
		} else {
			writeError(w, &domain.ValidationError{Message: "tenant-scoped task storage is unavailable"})
			return
		}
	} else {
		// Dashboard session (Fase W): always project-scoped — projectIdentity(ctx) is nil for
		// "no project selected", which resolves to the pre-Fase-W "unassigned" bucket.
		tasks, err = h.tasks.ListRootsByProject(r.Context(), projectIdentity(r.Context()), 50)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]taskResponse, 0, len(tasks))
	for _, task := range tasks {
		response = append(response, taskResponseFrom(task))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("taskId")
	id, err := uuid.Parse(rawID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "taskId must be a valid UUID"})
		return
	}

	task, err := h.findTask(r, domain.TaskID{Value: id})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, taskResponseFrom(task))
}

// Execute enqueues an execution job and returns immediately (etapa 6.5's 202-Accepted
// contract) — the actual orchestration happens asynchronously, in a worker consuming the
// durable queue (see application.ProcessJobUseCase). Poll GET
// /tasks/{taskId}/executions/{executionId} (etapa G10) for the outcome.
func (h *TaskHandler) Execute(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("taskId")
	id, err := uuid.Parse(rawID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "taskId must be a valid UUID"})
		return
	}

	taskID := domain.TaskID{Value: id}
	if _, ok := FromContext(r.Context()); ok {
		if _, err := h.findTask(r, taskID); err != nil {
			writeError(w, err)
			return
		}
	}
	execution, err := h.enqueue.Enqueue(r.Context(), taskID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, executionResponseFrom(execution))
}

// LatestExecution resolves a task's execution without the caller needing to already know its
// id (Fase E — the dashboard's task list only has task ids). Relies on the current 1:1
// Task→Execution relationship, same assumption ExecutionRepository.FindByTaskID documents.
func (h *TaskHandler) LatestExecution(w http.ResponseWriter, r *http.Request) {
	rawTaskID := r.PathValue("taskId")
	taskID, err := uuid.Parse(rawTaskID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "taskId must be a valid UUID"})
		return
	}

	if _, ok := FromContext(r.Context()); ok {
		if _, err := h.findTask(r, domain.TaskID{Value: taskID}); err != nil {
			writeError(w, err)
			return
		}
	}
	execution, err := h.executions.FindByTaskID(r.Context(), domain.TaskID{Value: taskID})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, executionResponseFrom(execution))
}

// GetExecution polls the outcome of an enqueued execution (etapa G10). 404s with
// execution_not_found both when the id doesn't exist at all and when it exists but belongs to
// a different task, matching the same non-leaking style as the other 404s.
func (h *TaskHandler) GetExecution(w http.ResponseWriter, r *http.Request) {
	rawTaskID := r.PathValue("taskId")
	taskID, err := uuid.Parse(rawTaskID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "taskId must be a valid UUID"})
		return
	}
	rawExecutionID := r.PathValue("executionId")
	executionID, err := uuid.Parse(rawExecutionID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "executionId must be a valid UUID"})
		return
	}

	execution, err := h.executions.FindByID(r.Context(), domain.ExecutionID{Value: executionID})
	if err != nil {
		writeError(w, err)
		return
	}
	if execution.TaskID.Value != taskID {
		writeError(w, application.ErrExecutionNotFound)
		return
	}
	if _, ok := FromContext(r.Context()); ok {
		if _, err := h.findTask(r, domain.TaskID{Value: taskID}); err != nil {
			writeError(w, application.ErrExecutionNotFound)
			return
		}
	}
	writeJSON(w, http.StatusOK, executionResponseFrom(execution))
}

func (h *TaskHandler) findTask(r *http.Request, id domain.TaskID) (domain.Task, error) {
	if identity, ok := tenantIdentity(r.Context()); ok {
		if scoped, supported := h.tasks.(tenantTaskReader); supported {
			return scoped.FindByIDForTenant(r.Context(), id, identity.TenantID)
		}
		return domain.Task{}, &domain.ValidationError{Message: "tenant-scoped task storage is unavailable"}
	}
	task, err := h.tasks.FindByID(r.Context(), id)
	if err != nil {
		return domain.Task{}, err
	}
	if !taskVisibleToCaller(r.Context(), task) {
		return domain.Task{}, application.ErrTaskNotFound
	}
	return task, nil
}

// taskVisibleToCaller confines an API key (Fase I) to its own project's tasks on every by-id
// read — a key holder knowing some other task's UUID must get the same 404 as a wrong UUID.
// Dashboard and tenant callers are unaffected (they have their own scoping paths).
func taskVisibleToCaller(ctx context.Context, task domain.Task) bool {
	identity, ok := FromContext(ctx)
	if !ok || identity.TokenUse != tokenUseApiKey {
		return true
	}
	return task.ProjectID != nil && identity.ProjectID != nil && task.ProjectID.Value == *identity.ProjectID
}

// tenantIdentity returns the caller's identity only when it actually carries a tenant to scope
// by — a dashboard session (token_use="dashboard") is a human operator of this single Prelo
// instance, not bound to any one messaging-core tenant, so it takes the same unscoped path as
// having no auth context at all (full visibility across every tenant's tasks).
func tenantIdentity(ctx context.Context) (AuthContext, bool) {
	identity, ok := FromContext(ctx)
	if !ok || identity.TokenUse == "dashboard" || identity.TokenUse == tokenUseApiKey {
		return AuthContext{}, false
	}
	return identity, true
}

func executionResponseFrom(e domain.Execution) executionResponse {
	resp := executionResponse{
		ExecutionID: e.ID.String(),
		TaskID:      e.TaskID.String(),
		AgentID:     e.AgentID.String(),
		Status:      string(e.Status),
		Result:      e.Result,
		Error:       e.Error,
		RequestID:   e.RequestID,
		Model:       e.Model,
		Provider:    e.Provider,
	}
	if e.StartedAt != nil {
		s := e.StartedAt.Format(time.RFC3339)
		resp.StartedAt = &s
	}
	if e.CompletedAt != nil {
		c := e.CompletedAt.Format(time.RFC3339)
		resp.CompletedAt = &c
	}
	return resp
}

func taskResponseFrom(t domain.Task) taskResponse {
	resp := taskResponse{
		ID:          t.ID.String(),
		Description: t.Description,
		Status:      string(t.Status),
		AgentID:     t.AgentID.String(),
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		Source:      string(t.Source),
	}
	if t.ProjectID != nil {
		id := t.ProjectID.String()
		resp.ProjectID = &id
	}
	resp.ClientID = clientIDString(t.ClientID)
	resp.ContactAddress = t.ContactAddress
	return resp
}
