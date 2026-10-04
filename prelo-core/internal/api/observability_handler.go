package api

import (
	"context"
	"net/http"
	"sort"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// ObservabilityHandler exposes the Fase D read paths: the turn-by-turn ledger of one execution
// (what the agent loop actually did — every LLM call, tool call, and delegation, in order) and
// the delegation tree rooted at a task (Fase C's parent/child chain). Both are pure reads over
// what Fases A-C already persist; there is no new write path here.
type ObservabilityHandler struct {
	tasks      application.TaskRepository
	executions application.ExecutionRepository
	turns      application.ExecutionTurnRepository
}

func NewObservabilityHandler(tasks application.TaskRepository, executions application.ExecutionRepository, turns application.ExecutionTurnRepository) *ObservabilityHandler {
	return &ObservabilityHandler{tasks: tasks, executions: executions, turns: turns}
}

// Turns returns the full ledger for one execution, oldest first — including turns still open
// (no outcome yet) for an execution currently AWAITING_RESUME, which is exactly what makes this
// useful as a "what is this execution waiting on" view, not just a post-mortem.
func (h *ObservabilityHandler) Turns(w http.ResponseWriter, r *http.Request) {
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
	if identity, ok := tenantIdentity(r.Context()); ok {
		if scoped, supported := h.tasks.(tenantTaskReader); !supported {
			writeError(w, &domain.ValidationError{Message: "tenant-scoped task storage is unavailable"})
			return
		} else if _, err := scoped.FindByIDForTenant(r.Context(), domain.TaskID{Value: taskID}, identity.TenantID); err != nil {
			writeError(w, application.ErrExecutionNotFound)
			return
		}
	} else if task, err := h.tasks.FindByID(r.Context(), domain.TaskID{Value: taskID}); err != nil || !taskVisibleToCaller(r.Context(), task) {
		writeError(w, application.ErrExecutionNotFound)
		return
	}

	turns, err := h.turns.ListByExecution(r.Context(), execution.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	sort.SliceStable(turns, func(i, j int) bool { return turns[i].TurnNumber < turns[j].TurnNumber })
	response := make([]turnResponse, 0, len(turns))
	for _, turn := range turns {
		response = append(response, turnResponseFrom(turn))
	}
	writeJSON(w, http.StatusOK, response)
}

// Tree returns the delegation subtree rooted at taskId — the root task plus every descendant
// created by delegate_to_agent (Fase C), each annotated with its own current execution status.
// Bounded in practice by domain.MaxDelegationDepth, so this never needs pagination.
func (h *ObservabilityHandler) Tree(w http.ResponseWriter, r *http.Request) {
	rawTaskID := r.PathValue("taskId")
	taskID, err := uuid.Parse(rawTaskID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "taskId must be a valid UUID"})
		return
	}

	var root domain.Task
	if identity, ok := tenantIdentity(r.Context()); ok {
		scoped, supported := h.tasks.(tenantTaskReader)
		if !supported {
			writeError(w, &domain.ValidationError{Message: "tenant-scoped task storage is unavailable"})
			return
		}
		root, err = scoped.FindByIDForTenant(r.Context(), domain.TaskID{Value: taskID}, identity.TenantID)
	} else {
		root, err = h.tasks.FindByID(r.Context(), domain.TaskID{Value: taskID})
		if err == nil && !taskVisibleToCaller(r.Context(), root) {
			err = application.ErrTaskNotFound
		}
	}
	if err != nil {
		writeError(w, err)
		return
	}

	node, err := h.buildTreeNode(r.Context(), root)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (h *ObservabilityHandler) buildTreeNode(ctx context.Context, task domain.Task) (taskTreeNode, error) {
	node := taskTreeNodeFrom(task)

	if execution, err := h.executions.FindByTaskID(ctx, task.ID); err == nil {
		status := string(execution.Status)
		node.ExecutionStatus = &status
	}

	children, err := h.tasks.FindChildren(ctx, task.ID)
	if err != nil {
		return taskTreeNode{}, err
	}
	for _, child := range children {
		childNode, err := h.buildTreeNode(ctx, child)
		if err != nil {
			return taskTreeNode{}, err
		}
		node.Children = append(node.Children, childNode)
	}
	return node, nil
}
