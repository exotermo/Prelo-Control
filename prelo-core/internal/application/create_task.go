package application

import (
	"context"
	"strings"

	"github.com/exotermo/prelo-core/internal/domain"
)

var defaultAgentID = domain.AgentID{Value: "general"}

// CreateTaskUseCase mirrors CreateTaskUseCase.java.
type CreateTaskUseCase struct {
	tasks         TaskRepository
	manualContext ManualContextRepository
	agents        AgentRegistry
	projects      ProjectReader
	contacts      ContactResolver
}

// ContactResolver recognizes a WhatsApp sender as a client (Fase C2).
type ContactResolver interface {
	FindByContactKeys(ctx context.Context, keys []string) (*domain.ClientID, error)
	ListProjects(ctx context.Context, clientID domain.ClientID) ([]domain.Project, error)
}

// SetContactResolver enables Fase C2: messages from a known contact are tied to their client.
func (uc *CreateTaskUseCase) SetContactResolver(contacts ContactResolver) { uc.contacts = contacts }

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
	return uc.create(ctx, description, items, agentID, "", source, projectID, nil)
}

// CreateForTenant is used by the authenticated HTTP API. Tenant ownership is assigned from
// verified JWT claims, never from request JSON or headers. projectID is always nil on this
// path in practice — the messaging-bridge tenant integration has no Fase W project context —
// but the parameter is accepted uniformly since the handler resolves it the same way either way.
func (uc *CreateTaskUseCase) CreateForTenant(ctx context.Context, tenantID string, description string, items []domain.ManualContextItem, agentID *domain.AgentID, source domain.TaskSource, projectID *domain.ProjectID) (domain.Task, error) {
	if tenantID == "" {
		return domain.Task{}, &domain.ValidationError{Message: "tenant identity is required"}
	}
	return uc.create(ctx, description, items, agentID, tenantID, source, projectID, nil)
}

// CreateForTenantFromContact is CreateForTenant for an inbound message (Fase C2): the sender's
// address is stored on the task and, when it belongs to a client, the task is tied to that
// client — and placed in the client's project when the client has exactly one (with several,
// a human picks; it stays unassigned).
func (uc *CreateTaskUseCase) CreateForTenantFromContact(ctx context.Context, tenantID string, description string, items []domain.ManualContextItem, agentID *domain.AgentID, projectID *domain.ProjectID, contactAddress string) (domain.Task, error) {
	if tenantID == "" {
		return domain.Task{}, &domain.ValidationError{Message: "tenant identity is required"}
	}
	return uc.create(ctx, description, items, agentID, tenantID, domain.TaskSourceMessaging, projectID, &contactAddress)
}

type contactMatch struct {
	address   *string
	clientID  *domain.ClientID
	projectID *domain.ProjectID
}

func (uc *CreateTaskUseCase) matchContact(ctx context.Context, raw string, projectID *domain.ProjectID) (contactMatch, error) {
	canonical, err := domain.CanonicalWhatsAppAddress(raw)
	if err != nil {
		// Unrecognized format: keep what was sent (bounded) so a human can still see who it was.
		trimmed := strings.TrimSpace(raw)
		if len(trimmed) > 80 {
			trimmed = trimmed[:80]
		}
		if trimmed == "" {
			return contactMatch{}, nil
		}
		return contactMatch{address: &trimmed}, nil
	}
	match := contactMatch{address: &canonical}
	if uc.contacts == nil {
		return match, nil
	}
	clientID, err := uc.contacts.FindByContactKeys(ctx, domain.ContactMatchKeys(canonical))
	if err != nil || clientID == nil {
		return match, err
	}
	match.clientID = clientID
	if projectID == nil {
		projects, err := uc.contacts.ListProjects(ctx, *clientID)
		if err != nil {
			return match, err
		}
		if len(projects) == 1 {
			match.projectID = &projects[0].ID
		}
	}
	return match, nil
}

func (uc *CreateTaskUseCase) create(ctx context.Context, description string, items []domain.ManualContextItem, agentID *domain.AgentID, tenantID string, source domain.TaskSource, projectID *domain.ProjectID, contactAddress *string) (domain.Task, error) {
	var match contactMatch
	if contactAddress != nil {
		var err error
		if match, err = uc.matchContact(ctx, *contactAddress, projectID); err != nil {
			return domain.Task{}, err
		}
		if match.projectID != nil {
			projectID = match.projectID
		}
	}
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
	task.ClientID = match.clientID
	task.ContactAddress = match.address
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
