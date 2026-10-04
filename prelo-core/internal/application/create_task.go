package application

import (
	"context"

	"github.com/exotermo/prelo-core/internal/domain"
)

var defaultAgentID = domain.AgentID{Value: "general"}

// CreateTaskUseCase mirrors CreateTaskUseCase.java.
type CreateTaskUseCase struct {
	tasks         TaskRepository
	manualContext ManualContextRepository
	agents        AgentRegistry
	projects      ProjectReader
}

// ProjectReader is what Fase PA needs from projects: their settings (default agent, instructions).
type ProjectReader interface {
	FindByID(ctx context.Context, id domain.ProjectID) (domain.Project, error)
}

// SetProjectReader enables per-project default agents (Fase PA); without it the default is "general".
func (uc *CreateTaskUseCase) SetProjectReader(projects ProjectReader) { uc.projects = projects }

func NewCreateTaskUseCase(tasks TaskRepository, manualContext ManualContextRepository, agents AgentRegistry) *CreateTaskUseCase {
	return &CreateTaskUseCase{tasks: tasks, manualContext: manualContext, agents: agents}
}

// Create resolves agentID to "general" when nil/blank. It throws (returns) an unknown-agent
// error before anything is persisted if the id doesn't resolve in the curated catalog — the
// client only ever picks an id; directive, model, and capabilities always come from the
// catalog, never from the request. projectID (Fase W) is resolved by the handler from the
// X-Project-Id header, never from request JSON — nil means the "unassigned" bucket.
func (uc *CreateTaskUseCase) Create(ctx context.Context, description string, items []domain.ManualContextItem, agentID *domain.AgentID, source domain.TaskSource, projectID *domain.ProjectID) (domain.Task, error) {
	return uc.create(ctx, description, items, agentID, "", source, projectID)
}

// CreateForTenant is used by the authenticated HTTP API. Tenant ownership is assigned from
// verified JWT claims, never from request JSON or headers. projectID is always nil on this
// path in practice — the messaging-bridge tenant integration has no Fase W project context —
// but the parameter is accepted uniformly since the handler resolves it the same way either way.
func (uc *CreateTaskUseCase) CreateForTenant(ctx context.Context, tenantID string, description string, items []domain.ManualContextItem, agentID *domain.AgentID, source domain.TaskSource, projectID *domain.ProjectID) (domain.Task, error) {
	if tenantID == "" {
		return domain.Task{}, &domain.ValidationError{Message: "tenant identity is required"}
	}
	return uc.create(ctx, description, items, agentID, tenantID, source, projectID)
}

func (uc *CreateTaskUseCase) create(ctx context.Context, description string, items []domain.ManualContextItem, agentID *domain.AgentID, tenantID string, source domain.TaskSource, projectID *domain.ProjectID) (domain.Task, error) {
	resolvedAgentID := defaultAgentID
	if agentID != nil {
		resolvedAgentID = *agentID
	} else if projectID != nil && uc.projects != nil {
		if project, err := uc.projects.FindByID(ctx, *projectID); err == nil && project.DefaultAgentID != nil {
			resolvedAgentID = domain.AgentID{Value: *project.DefaultAgentID}
		}
	}
	if _, err := uc.agents.FindRequired(resolvedAgentID); err != nil {
		return domain.Task{}, err
	}

	task, err := domain.NewTask(description, resolvedAgentID)
	if err != nil {
		return domain.Task{}, err
	}
	if source != "" {
		task = task.WithSource(source)
	}
	task.TenantID = tenantID
	task.ProjectID = projectID
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
