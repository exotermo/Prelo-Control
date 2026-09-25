package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

// DelegateTool is Fase C's orchestration primitive: "run" doesn't compute a result inline like
// every other tool — it creates a child Task for another curated agent and enqueues it through
// the exact same pipeline any top-level task goes through (including its own tool loop,
// recursively, up to domain.MaxDelegationDepth). What it returns (the child's TaskID) is not
// the real answer — RunAgentLoopUseCase/DecideApprovalUseCase special-case
// domain.DelegateToolName and suspend the caller (domain.SuspensionSubtask) instead of treating
// that id as a normal tool result; ProcessJobUseCase.resolveParentSubtaskSuspension is what
// eventually feeds the child's actual result back once it finishes.
type DelegateTool struct {
	tasks   application.TaskRepository
	agents  application.AgentRegistry
	enqueue *application.EnqueueExecutionUseCase
}

func NewDelegateTool(tasks application.TaskRepository, agents application.AgentRegistry, enqueue *application.EnqueueExecutionUseCase) *DelegateTool {
	return &DelegateTool{tasks: tasks, agents: agents, enqueue: enqueue}
}

func (DelegateTool) Definition() domain.ToolDefinition {
	def, _ := domain.NewToolDefinition(domain.DelegateToolName,
		"Delegates a piece of work to another curated agent as a child task, up to a bounded delegation depth. "+
			"Args: {\"agentId\": \"<curated agent id>\", \"description\": \"<what the delegate should do>\"}. "+
			"The result is not available inline — the caller resumes automatically once the delegate finishes.",
		domain.RiskModerate)
	return def
}

type delegateArgs struct {
	AgentID     string `json:"agentId"`
	Description string `json:"description"`
}

func (t *DelegateTool) Execute(ctx context.Context, execution domain.Execution, argsJSON string) (string, error) {
	var args delegateArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid delegate_to_agent args: %w", err)
	}

	targetAgentID, err := domain.NewAgentID(args.AgentID)
	if err != nil {
		return "", err
	}
	if _, err := t.agents.FindRequired(targetAgentID); err != nil {
		return "", err
	}

	parent, err := t.tasks.FindByID(ctx, execution.TaskID)
	if err != nil {
		return "", err
	}

	child, err := domain.NewSubtask(args.Description, targetAgentID, parent)
	if err != nil {
		return "", err
	}
	if err := t.tasks.Insert(ctx, child); err != nil {
		return "", err
	}
	if _, err := t.enqueue.Enqueue(ctx, child.ID); err != nil {
		return "", err
	}

	return child.ID.String(), nil
}
