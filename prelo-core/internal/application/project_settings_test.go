package application

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

type settingsProjects struct{ project domain.Project }

func (s settingsProjects) FindByID(context.Context, domain.ProjectID) (domain.Project, error) {
	return s.project, nil
}

type captureTasks struct {
	TaskRepository
	inserted domain.Task
}

func (c *captureTasks) Insert(_ context.Context, t domain.Task) error {
	c.inserted = t
	return nil
}

type noManualContext struct{}

func (noManualContext) Save(context.Context, domain.TaskID, []domain.ManualContextItem) error { return nil }
func (noManualContext) FindByTaskID(context.Context, domain.TaskID) ([]domain.ManualContextItem, error) {
	return nil, nil
}

type noSnapshots struct{ ContextSnapshotRepository }

func (noSnapshots) FindLatestByTaskID(context.Context, domain.TaskID) (domain.ContextSnapshot, bool, error) {
	return domain.ContextSnapshot{}, false, nil
}

func ptr(s string) *string { return &s }

func TestCreateTask_UsesTheProjectDefaultAgentOnlyWhenNoneIsChosen(t *testing.T) {
	agents := &fakeAgents{agents: map[string]domain.AgentDefinition{"general": {}, "concise": {}}}
	project := domain.Project{ID: domain.ProjectID{Value: uuid.New()}, DefaultAgentID: ptr("concise")}
	tasks := &captureTasks{}
	uc := NewCreateTaskUseCase(tasks, noManualContext{}, agents)
	uc.SetProjectReader(settingsProjects{project: project})

	if _, err := uc.Create(context.Background(), "x", nil, nil, domain.TaskSourceManual, &project.ID); err != nil {
		t.Fatal(err)
	}
	if tasks.inserted.AgentID.Value != "concise" {
		t.Fatalf("expected project default agent, got %s", tasks.inserted.AgentID.Value)
	}
	explicit := domain.AgentID{Value: "general"}
	if _, err := uc.Create(context.Background(), "x", nil, &explicit, domain.TaskSourceManual, &project.ID); err != nil {
		t.Fatal(err)
	}
	if tasks.inserted.AgentID.Value != "general" {
		t.Fatalf("an explicit agent must win, got %s", tasks.inserted.AgentID.Value)
	}
}

func TestContextResolver_AddsProjectInstructions(t *testing.T) {
	project := domain.Project{ID: domain.ProjectID{Value: uuid.New()}, Instructions: ptr("Responda sempre em português formal.")}
	resolver := NewContextResolver(noManualContext{}, noSnapshots{})
	resolver.SetProjectReader(settingsProjects{project: project})
	task, _ := domain.NewTask("resuma o contrato", domain.AgentID{Value: "general"})
	task.ProjectID = &project.ID

	snapshot, err := resolver.Resolve(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range snapshot.Items {
		if item.Provenance == "project-instructions" && item.Content == *project.Instructions {
			found = true
		}
	}
	if !found {
		t.Fatalf("instructions missing from context: %+v", snapshot.Items)
	}
}

func TestProjectWithSettings_Validates(t *testing.T) {
	p := domain.Project{CoverColor: "ink"}
	if _, err := p.WithSettings("", nil, nil, nil, "ink"); err == nil {
		t.Fatal("blank name must be refused")
	}
	if _, err := p.WithSettings("ok", nil, nil, nil, "neon"); err == nil {
		t.Fatal("unknown color must be refused")
	}
	long := string(make([]byte, domain.MaxProjectInstructionsLength+1))
	if _, err := p.WithSettings("ok", nil, nil, &long, "ink"); err == nil {
		t.Fatal("instructions over the limit must be refused")
	}
	got, err := p.WithSettings("Novo", ptr("  "), ptr(""), ptr("x"), "moss")
	if err != nil || got.Description != nil || got.DefaultAgentID != nil || got.CoverColor != "moss" {
		t.Fatalf("blank optional settings must clear: %+v %v", got, err)
	}
}
