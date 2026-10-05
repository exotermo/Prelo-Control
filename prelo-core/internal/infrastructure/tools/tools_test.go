package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/webhook"
)

type fakeTasks struct{ task domain.Task }

func (f fakeTasks) FindByID(context.Context, domain.TaskID) (domain.Task, error) { return f.task, nil }

type fakeClients struct {
	client   domain.Client
	contacts []domain.ClientContact
	updated  *domain.Client
}

func (f *fakeClients) FindByID(context.Context, domain.ClientID) (domain.Client, error) {
	return f.client, nil
}
func (f *fakeClients) ListContacts(context.Context, domain.ClientID) ([]domain.ClientContact, error) {
	return f.contacts, nil
}
func (f *fakeClients) ListProjects(context.Context, domain.ClientID) ([]domain.Project, error) {
	return nil, nil
}
func (f *fakeClients) Update(_ context.Context, c domain.Client) (domain.Client, error) {
	f.updated = &c
	return c, nil
}

type fakeSender struct{ to, text string }

func (f *fakeSender) SendWhatsApp(_ context.Context, to, text string) error {
	f.to, f.text = to, text
	return nil
}

func execution() domain.Execution {
	agent, _ := domain.NewAgentID("general")
	return domain.Execution{ID: domain.NewExecutionID(), TaskID: domain.NewTaskID(), AgentID: agent}
}

func TestEveryToolDeclaresASchemaAndAnImpact(t *testing.T) {
	all := []interface{ Definition() domain.ToolDefinition }{
		NewCurrentTimeTool(), NewEchoTool(), &DelegateTool{}, &SearchWorkspaceTool{}, &GetClientTool{}, &AddClientNoteTool{},
		&ListServersTool{}, &CheckServerHealthTool{}, &InspectWebsiteTool{}, &SendWhatsAppTool{},
	}
	for _, tool := range all {
		def := tool.Definition()
		if !json.Valid(def.InputSchema) || def.Impact == "" {
			t.Errorf("%s: schema/impact missing", def.Name)
		}
	}
	if (&SendWhatsAppTool{}).Definition().RiskLevel != domain.RiskHigh || (&AddClientNoteTool{}).Definition().RiskLevel != domain.RiskModerate {
		t.Fatal("writes must require approval (MODERATE/HIGH)")
	}
}

func TestValidateToolArgs(t *testing.T) {
	def := (&AddClientNoteTool{}).Definition()
	if err := domain.ValidateToolArgs(def, `{"clientId":"x","note":"oi"}`); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{"clientId":"x"}`, `{"clientId":1,"note":"a"}`, `{"clientId":"x","note":"a","extra":true}`, `[1]`} {
		if domain.ValidateToolArgs(def, bad) == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
	if domain.ValidateToolArgs((&SearchWorkspaceTool{}).Definition(), `{"query":"ab","types":"pets"}`) == nil {
		t.Error("enum must be enforced")
	}
}

func TestAddClientNoteAppendsDatedNote(t *testing.T) {
	old := "nota antiga"
	clients := &fakeClients{client: domain.Client{ID: domain.NewClientID(), Name: "Padaria", Notes: &old}}
	tool := NewAddClientNoteTool(clients)
	tool.now = func() time.Time { return time.Date(2026, 10, 4, 9, 30, 0, 0, time.UTC) }
	out, err := tool.Execute(context.Background(), execution(), `{"clientId":"`+clients.client.ID.String()+`","note":"ligar amanhã"}`)
	if err != nil || !strings.Contains(out, "Padaria") {
		t.Fatalf("%s %v", out, err)
	}
	if got := *clients.updated.Notes; got != "nota antiga\n\n[04/10/2026 09:30 · agente general] ligar amanhã" {
		t.Fatalf("notes = %q", got)
	}
}

func TestSendWhatsAppRespectsOptOutAndPrefersWhatsAppContact(t *testing.T) {
	id := domain.NewClientID()
	clients := &fakeClients{client: domain.Client{ID: id, Name: "Padaria"}, contacts: []domain.ClientContact{
		{Kind: domain.ContactKindPhone, Value: "+554133334444", IsPrimary: true},
		{Kind: domain.ContactKindWhatsApp, Value: "+5541984450529"},
	}}
	sender := &fakeSender{}
	tool := NewSendWhatsAppTool(clients, sender)
	args := `{"clientId":"` + id.String() + `","text":"Olá!"}`
	if _, err := tool.Execute(context.Background(), execution(), args); err != nil {
		t.Fatal(err)
	}
	if sender.to != "5541984450529" || sender.text != "Olá!" {
		t.Fatalf("sent to %q: %q", sender.to, sender.text)
	}
	now := time.Now()
	clients.client.OptedOutAt = &now
	sender.to = ""
	if _, err := tool.Execute(context.Background(), execution(), args); err == nil || sender.to != "" {
		t.Fatal("opted-out client must never be messaged")
	}
}

type fakeServers struct{ servers map[uuid.UUID]domain.Server }

func (f fakeServers) FindByID(_ context.Context, id domain.ServerID) (domain.Server, error) {
	s, ok := f.servers[id.Value]
	if !ok {
		return domain.Server{}, application.ErrServerNotFound
	}
	return s, nil
}
func (f fakeServers) ListByProject(context.Context, *uuid.UUID) ([]domain.Server, error) {
	return nil, nil
}

type fakeHealth struct{ called bool }

func (f *fakeHealth) Check(context.Context, domain.ServerID) (application.ServerHealthSnapshot, error) {
	f.called = true
	return application.ServerHealthSnapshot{Status: domain.ServerStatus("ONLINE")}, nil
}

func TestCheckServerHealthNeverReachesAnotherProjectsServer(t *testing.T) {
	mine, other := domain.NewProjectID(), domain.NewProjectID()
	server := domain.Server{ID: domain.NewServerID(), Name: "web", ProjectID: &other}
	health := &fakeHealth{}
	tool := NewCheckServerHealthTool(fakeTasks{task: domain.Task{ProjectID: &mine}}, fakeServers{servers: map[uuid.UUID]domain.Server{server.ID.Value: server}}, health)
	if _, err := tool.Execute(context.Background(), execution(), `{"serverId":"`+server.ID.String()+`"}`); err == nil || health.called {
		t.Fatal("a server of another project must look like not found")
	}
	server.ProjectID = &mine
	tool.servers = fakeServers{servers: map[uuid.UUID]domain.Server{server.ID.Value: server}}
	if _, err := tool.Execute(context.Background(), execution(), `{"serverId":"`+server.ID.String()+`"}`); err != nil || !health.called {
		t.Fatalf("own project's server should be checked: %v", err)
	}
}

func TestInspectWebsiteReportsPresenceAndRefusesInternalAddresses(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>Padaria São João</title><meta name="viewport" content="width=device-width, initial-scale=1">
			<meta name="description" content="Pães &amp; doces"></head><body><a href="https://wa.me/5541999">Peça</a></body></html>`))
	}))
	defer site.Close()
	open := NewInspectWebsiteTool(webhook.NewHTTPClient(true))
	out, err := open.Execute(context.Background(), execution(), `{"url":"`+site.URL+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	_ = json.Unmarshal([]byte(out), &report)
	if report["title"] != "Padaria São João" || report["mobileFriendly"] != true || report["description"] != "Pães & doces" || report["https"] != false {
		t.Fatalf("report = %v", report)
	}
	if social, _ := report["social"].(map[string]any); social["whatsapp"] == nil {
		t.Fatalf("whatsapp link not found: %v", report)
	}
	guarded := NewInspectWebsiteTool(webhook.NewHTTPClient(false))
	out, _ = guarded.Execute(context.Background(), execution(), `{"url":"`+site.URL+`"}`)
	if !strings.Contains(out, "interno") {
		t.Fatalf("loopback must be refused, got %s", out)
	}
}
