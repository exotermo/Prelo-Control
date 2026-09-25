package application

import (
	"context"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

var defaultAgentID = domain.AgentID{Value: "general"}

// CreateTaskUseCase mirrors CreateTaskUseCase.java.
type CreateTaskUseCase struct {
	tasks         TaskRepository
	manualContext ManualContextRepository
	agents        AgentRegistry
}

func NewCreateTaskUseCase(tasks TaskRepository, manualContext ManualContextRepository, agents AgentRegistry) *CreateTaskUseCase {
	return &CreateTaskUseCase{tasks: tasks, manualContext: manualContext, agents: agents}
}

// Create resolves agentID to "general" when nil/blank. It throws (returns) an unknown-agent
// error before anything is persisted if the id doesn't resolve in the curated catalog — the
// client only ever picks an id; directive, model, and capabilities always come from the
// catalog, never from the request.
func (uc *CreateTaskUseCase) Create(ctx context.Context, description string, items []domain.ManualContextItem, agentID *domain.AgentID) (domain.Task, error) {
	return uc.create(ctx, description, items, agentID, "")
}

// CreateForTenant is used by the authenticated HTTP API. Tenant ownership is assigned from
// verified JWT claims, never from request JSON or headers.
func (uc *CreateTaskUseCase) CreateForTenant(ctx context.Context, tenantID string, description string, items []domain.ManualContextItem, agentID *domain.AgentID) (domain.Task, error) {
	if tenantID == "" {
		return domain.Task{}, &domain.ValidationError{Message: "tenant identity is required"}
	}
	return uc.create(ctx, description, items, agentID, tenantID)
}

func (uc *CreateTaskUseCase) create(ctx context.Context, description string, items []domain.ManualContextItem, agentID *domain.AgentID, tenantID string) (domain.Task, error) {
	resolvedAgentID := defaultAgentID
	if agentID != nil {
		resolvedAgentID = *agentID
	}
	if _, err := uc.agents.FindRequired(resolvedAgentID); err != nil {
		return domain.Task{}, err
	}

	task, err := domain.NewTask(description, resolvedAgentID)
	if err != nil {
		return domain.Task{}, err
	}
	task.TenantID = tenantID
	if err := uc.tasks.Insert(ctx, task); err != nil {
		return domain.Task{}, err
	}
	if len(items) > 0 {
		if err := uc.manualContext.Save(ctx, task.ID, items); err != nil {
			return domain.Task{}, err
		}
	}
	return task, nil
}
