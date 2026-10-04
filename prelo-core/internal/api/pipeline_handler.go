package api

import (
	"context"
	"net/http"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// pipelineRootLimit caps how many active roots the Pipeline page asks for in one go — the same
// bound ListRoots already uses for the Tasks page's full (not just active) list.
const pipelineRootLimit = 50

// PipelineHandler backs the Fase P Pipeline page — one global, auto-refreshing view of every
// delegation tree that is currently active (root Task.Status not yet terminal), each node
// resolved down to a single human-facing status instead of the raw Task/Execution/
// ExecutionSuspension triple that actually produces it.
type PipelineHandler struct {
	tasks       application.TaskRepository
	executions  application.ExecutionRepository
	suspensions application.ExecutionSuspensionRepository
}

func NewPipelineHandler(tasks application.TaskRepository, executions application.ExecutionRepository, suspensions application.ExecutionSuspensionRepository) *PipelineHandler {
	return &PipelineHandler{tasks: tasks, executions: executions, suspensions: suspensions}
}

// Get returns one pipelineNodeResponse tree per active root, newest first — project-scoped
// (Fase W); projectIdentity(ctx) nil means the "unassigned" bucket.
func (h *PipelineHandler) Get(w http.ResponseWriter, r *http.Request) {
	roots, err := h.tasks.ListActiveRootsByProject(r.Context(), projectIdentity(r.Context()), pipelineRootLimit)
	if err != nil {
		writeError(w, err)
		return
	}

	response := make([]pipelineNodeResponse, 0, len(roots))
	for _, root := range roots {
		node, err := h.buildPipelineNode(r.Context(), root)
		if err != nil {
			writeError(w, err)
			return
		}
		response = append(response, node)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *PipelineHandler) buildPipelineNode(ctx context.Context, task domain.Task) (pipelineNodeResponse, error) {
	node := pipelineNodeFrom(task, h.resolveStatus(ctx, task))

	children, err := h.tasks.FindChildren(ctx, task.ID)
	if err != nil {
		return pipelineNodeResponse{}, err
	}
	for _, child := range children {
		childNode, err := h.buildPipelineNode(ctx, child)
		if err != nil {
			return pipelineNodeResponse{}, err
		}
		node.Children = append(node.Children, childNode)
	}
	return node, nil
}

// resolveStatus passes CREATED/QUEUED/COMPLETED/FAILED straight through — only a RUNNING task
// needs the extra lookup, since that's the only state an ExecutionSuspension can be hiding
// behind (AWAITING_RESUME has no Task.Status of its own; the Task just stays RUNNING the whole
// time it's suspended).
func (h *PipelineHandler) resolveStatus(ctx context.Context, task domain.Task) string {
	if task.Status != domain.TaskRunning {
		return string(task.Status)
	}

	execution, err := h.executions.FindByTaskID(ctx, task.ID)
	if err != nil {
		return string(task.Status)
	}

	suspension, suspended, err := h.suspensions.FindActiveByExecutionID(ctx, execution.ID)
	if err != nil || !suspended {
		return string(task.Status)
	}

	switch suspension.Reason {
	case domain.SuspensionApproval:
		return "AWAITING_APPROVAL"
	case domain.SuspensionSubtask:
		return "AWAITING_SUBTASK"
	default:
		return string(task.Status)
	}
}
