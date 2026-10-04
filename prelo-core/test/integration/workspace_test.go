package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
)

// Fase C1: clients, contacts, project link, task inheritance, accent-insensitive search with
// project visibility, recents and the client timeline — against the real migrations.
func TestWorkspace_ClientsSearchRecentTimeline(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	clients := postgres.NewClientRepository(pool)
	projects := postgres.NewProjectRepository(pool)
	members := postgres.NewProjectMemberRepository(pool)
	users := postgres.NewDashboardUserRepository(pool)
	tasks := postgres.NewTaskRepository(pool)
	reads := postgres.NewWorkspaceReadModel(pool)
	svc := application.NewWorkspaceService(clients, members, reads)

	operator, err := domain.NewDashboardUser("op@example.com", domain.DashboardRoleOperator)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.Insert(ctx, operator); err != nil {
		t.Fatal(err)
	}

	company := "São João Ltda"
	client, err := svc.CreateClient(ctx, domain.ClientFields{Name: "Padaria São João", Company: &company},
		[]application.ContactInput{{Kind: domain.ContactKindWhatsApp, Value: "(41) 98445-0529"}}, "tester")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	// The same number on another client is refused.
	if _, err := svc.CreateClient(ctx, domain.ClientFields{Name: "Outro"},
		[]application.ContactInput{{Kind: domain.ContactKindWhatsApp, Value: "+55 41 98445-0529"}}, "tester"); !errors.Is(err, application.ErrContactInUse) {
		t.Fatalf("expected ErrContactInUse, got %v", err)
	}

	visible, _ := domain.NewProject("Site da padaria", nil, "tester")
	hidden, _ := domain.NewProject("Projeto secreto da padaria", nil, "tester")
	for _, p := range []domain.Project{visible, hidden} {
		if err := projects.Insert(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err := clients.SetProjectClient(ctx, p.ID, &client.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := members.Add(ctx, domain.NewProjectMember(visible.ID, operator.ID, "tester")); err != nil {
		t.Fatal(err)
	}

	// Tasks inherit the client of their project.
	agent, _ := domain.NewAgentID("general")
	visibleTask, _ := domain.NewTask("Criar cardápio digital da padaria", agent)
	visibleTask.ProjectID = &visible.ID
	hiddenTask, _ := domain.NewTask("Orçamento sigiloso da padaria", agent)
	hiddenTask.ProjectID = &hidden.ID
	for _, tk := range []domain.Task{visibleTask, hiddenTask} {
		if err := tasks.Insert(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}
	var inherited *string
	if err := pool.QueryRow(ctx, `SELECT client_id::text FROM tasks WHERE id = $1`, visibleTask.ID.Value).Scan(&inherited); err != nil || inherited == nil || *inherited != client.ID.String() {
		t.Fatalf("task should inherit the project's client, got %v (%v)", inherited, err)
	}

	admin := application.Viewer{AllProjects: true}
	op := application.Viewer{UserID: operator.ID}

	// Accent- and case-insensitive, and by phone fragment.
	for _, q := range []string{"sao joao", "PADARIA", "98445-0529"} {
		res, err := svc.Search(ctx, admin, q, application.SearchTypes{Clients: true})
		if err != nil || len(res.Clients) != 1 || res.Clients[0].ID != client.ID.Value {
			t.Fatalf("search %q: %+v %v", q, res.Clients, err)
		}
	}
	// LIKE metacharacters are literal.
	if res, _ := svc.Search(ctx, admin, "%%", application.SearchTypes{Clients: true}); len(res.Clients) != 0 {
		t.Fatalf("%%%% should match nothing literally, got %d", len(res.Clients))
	}

	adminRes, _ := svc.Search(ctx, admin, "padaria", application.SearchTypes{})
	opRes, _ := svc.Search(ctx, op, "padaria", application.SearchTypes{})
	if len(adminRes.Projects) != 2 || len(adminRes.Tasks) != 2 {
		t.Fatalf("admin sees everything: projects=%d tasks=%d", len(adminRes.Projects), len(adminRes.Tasks))
	}
	if len(opRes.Projects) != 1 || len(opRes.Tasks) != 1 || opRes.Tasks[0].ID != visibleTask.ID.Value {
		t.Fatalf("operator must not find the other project's work: projects=%d tasks=%+v", len(opRes.Projects), opRes.Tasks)
	}

	// Recents: hidden project is filtered out for the operator; re-touching moves to the top.
	for _, ref := range []struct {
		kind string
		id   [16]byte
	}{{"PROJECT", hidden.ID.Value}, {"CLIENT", client.ID.Value}, {"PROJECT", visible.ID.Value}} {
		if err := svc.TouchRecent(ctx, op, ref.kind, ref.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.TouchRecent(ctx, op, "CLIENT", client.ID.Value); err != nil {
		t.Fatal(err)
	}
	home, err := svc.Home(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Recent) != 2 || home.Recent[0].Kind != "CLIENT" || home.Recent[1].ID != visible.ID.Value {
		t.Fatalf("recent = %+v", home.Recent)
	}

	// Timeline: client created + visible project + its task, nothing from the hidden project.
	entries, err := svc.Timeline(ctx, op, client.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, e := range entries {
		kinds[e.Kind]++
		if e.ID == hidden.ID.Value || e.ID == hiddenTask.ID.Value {
			t.Fatalf("timeline leaked hidden item %+v", e)
		}
	}
	if kinds["CLIENT_CREATED"] != 1 || kinds["PROJECT"] != 1 || kinds["TASK"] != 1 {
		t.Fatalf("timeline kinds = %v", kinds)
	}
	future := time.Now().Add(time.Hour)
	if page, _ := svc.Timeline(ctx, admin, client.ID, &future); len(page) != 5 {
		t.Fatalf("admin timeline should hold 5 entries, got %d", len(page))
	}

	// Deleting the client unlinks projects and frees the number.
	if err := clients.SoftDelete(ctx, client.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := projects.FindByID(ctx, visible.ID); p.ClientID != nil {
		t.Fatal("project should be unlinked after client delete")
	}
	if _, err := svc.CreateClient(ctx, domain.ClientFields{Name: "Novo dono"},
		[]application.ContactInput{{Kind: domain.ContactKindWhatsApp, Value: "41984450529"}}, "tester"); err != nil {
		t.Fatalf("number should be free again: %v", err)
	}
}
