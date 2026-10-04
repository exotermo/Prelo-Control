package integration

import (
	"context"
	"testing"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/agentregistry"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
)

const bridgeTenant = "00000000-0000-0000-0000-000000000000"

// Fase C2: a WhatsApp sender is recognized as a client (by WhatsApp ID or by phone, with or
// without the Brazilian ninth digit), lands in the client's only project, and conversations
// that arrived before the contact was registered join the client once it is.
func TestWhatsAppSenderBecomesClientWork(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	clients := postgres.NewClientRepository(pool)
	projects := postgres.NewProjectRepository(pool)
	tasks := postgres.NewTaskRepository(pool)
	agents, err := agentregistry.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	create := application.NewCreateTaskUseCase(tasks, postgres.NewManualContextRepository(pool), agents)
	create.SetContactResolver(clients)
	workspace := application.NewWorkspaceService(clients, postgres.NewProjectMemberRepository(pool), postgres.NewWorkspaceReadModel(pool))
	customer := domain.AgentID{Value: "customer"}

	lidClient, err := workspace.CreateClient(ctx, domain.ClientFields{Name: "Cliente LID"},
		[]application.ContactInput{{Kind: domain.ContactKindWhatsApp, Value: "26668123456789@lid"}}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	only, _ := domain.NewProject("Único projeto", nil, "tester")
	if err := projects.Insert(ctx, only); err != nil {
		t.Fatal(err)
	}
	if err := clients.SetProjectClient(ctx, only.ID, &lidClient.ID); err != nil {
		t.Fatal(err)
	}

	task, err := create.CreateForTenantFromContact(ctx, bridgeTenant, "Oi, tudo bem?", nil, &customer, nil, "26668123456789@lid")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := tasks.FindByID(ctx, task.ID)
	if got.ClientID == nil || got.ClientID.Value != lidClient.ID.Value || got.ProjectID == nil || got.ProjectID.Value != only.ID.Value {
		t.Fatalf("known WhatsApp ID should land on the client's only project: %+v", got)
	}
	if got.Source != domain.TaskSourceMessaging || got.ContactAddress == nil || *got.ContactAddress != "26668123456789@lid" {
		t.Fatalf("source/contact not stored: %+v", got)
	}

	// Phone with the ninth digit registered; message arrives from the old 8-digit JID.
	phoneClient, err := workspace.CreateClient(ctx, domain.ClientFields{Name: "Cliente telefone"},
		[]application.ContactInput{{Kind: domain.ContactKindWhatsApp, Value: "(41) 98445-0529"}}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := domain.NewProject("Segundo", nil, "tester")
	third, _ := domain.NewProject("Terceiro", nil, "tester")
	for _, p := range []domain.Project{second, third} {
		_ = projects.Insert(ctx, p)
		_ = clients.SetProjectClient(ctx, p.ID, &phoneClient.ID)
	}
	task, err = create.CreateForTenantFromContact(ctx, bridgeTenant, "Quero um orçamento", nil, &customer, nil, "554184450529@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	got, _ = tasks.FindByID(ctx, task.ID)
	if got.ClientID == nil || got.ClientID.Value != phoneClient.ID.Value {
		t.Fatalf("8-digit JID should match the 9-digit contact: %+v", got)
	}
	if got.ProjectID != nil {
		t.Fatal("a client with several projects must not have the message auto-filed")
	}

	// Unknown sender: stored, no client — until someone adds the contact.
	early, err := create.CreateForTenantFromContact(ctx, bridgeTenant, "Primeira mensagem", nil, &customer, nil, "99999000011111@lid")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ = tasks.FindByID(ctx, early.ID); got.ClientID != nil {
		t.Fatal("unknown sender must not be tied to a client")
	}
	if _, err := workspace.AddContact(ctx, lidClient.ID, application.ContactInput{Kind: domain.ContactKindWhatsApp, Value: "99999000011111@lid"}); err != nil {
		t.Fatal(err)
	}
	if got, _ = tasks.FindByID(ctx, early.ID); got.ClientID == nil || got.ClientID.Value != lidClient.ID.Value {
		t.Fatalf("earlier conversation should join the client once the contact is added: %+v", got)
	}
	timeline, err := workspace.Timeline(ctx, application.Viewer{AllProjects: true}, lidClient.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range timeline {
		found = found || e.ID == early.ID.Value
	}
	if !found {
		t.Fatal("backfilled conversation should be on the client's timeline")
	}
}
