package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// Fase T workspace tools. Clients are workspace-wide (every dashboard user sees all of them, same
// rule as the Clientes screen); project-bound data (tasks, files) follows the task's project.

type workspaceSearcher interface {
	Search(ctx context.Context, query string, types application.SearchTypes, visibility application.ProjectVisibility, limit int) (application.SearchResults, error)
}

type clientStore interface {
	FindByID(ctx context.Context, id domain.ClientID) (domain.Client, error)
	ListContacts(ctx context.Context, clientID domain.ClientID) ([]domain.ClientContact, error)
	ListProjects(ctx context.Context, clientID domain.ClientID) ([]domain.Project, error)
	Update(ctx context.Context, client domain.Client) (domain.Client, error)
}

// visibilityOf scopes project-bound results to the task's own project.
func visibilityOf(task domain.Task) application.ProjectVisibility {
	if task.ProjectID == nil {
		return application.ProjectVisibility{ProjectIDs: []uuid.UUID{}}
	}
	return application.ProjectVisibility{ProjectIDs: []uuid.UUID{task.ProjectID.Value}}
}

func parseClientID(raw string) (domain.ClientID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return domain.ClientID{}, &domain.ValidationError{Message: "clientId deve ser um UUID (use search_workspace para achá-lo)"}
	}
	return domain.ClientID{Value: id}, nil
}

// --- search_workspace ---

type SearchWorkspaceTool struct {
	tasks  taskReader
	search workspaceSearcher
}

func NewSearchWorkspaceTool(tasks taskReader, search workspaceSearcher) *SearchWorkspaceTool {
	return &SearchWorkspaceTool{tasks: tasks, search: search}
}

func (*SearchWorkspaceTool) Definition() domain.ToolDefinition {
	def, _ := domain.NewToolDefinition("search_workspace",
		"Busca no Prelo por clientes, projetos, tasks/conversas e arquivos (sem acento, por nome, cidade, telefone ou texto). "+
			"Retorna ids para usar em outras ferramentas. Só leitura.", domain.RiskLow)
	return def.WithSchema(`{"type":"object","properties":{"query":{"type":"string"},"types":{"type":"string","enum":["all","clients","projects","tasks","files"]}},"required":["query"],"additionalProperties":false}`,
		"Nenhum: só consulta dados do workspace.")
}

func (t *SearchWorkspaceTool) Execute(ctx context.Context, execution domain.Execution, argsJSON string) (string, error) {
	var args struct {
		Query string `json:"query"`
		Types string `json:"types"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if len([]rune(strings.TrimSpace(args.Query))) < 2 {
		return "", &domain.ValidationError{Message: "query precisa de pelo menos 2 caracteres"}
	}
	task, err := t.tasks.FindByID(ctx, execution.TaskID)
	if err != nil {
		return "", err
	}
	types := application.SearchTypes{Clients: true, Projects: true, Tasks: true, Files: true}
	switch args.Types {
	case "clients":
		types = application.SearchTypes{Clients: true}
	case "projects":
		types = application.SearchTypes{Projects: true}
	case "tasks":
		types = application.SearchTypes{Tasks: true}
	case "files":
		types = application.SearchTypes{Files: true}
	}
	results, err := t.search.Search(ctx, strings.TrimSpace(args.Query), types, visibilityOf(task), 8)
	if err != nil {
		return "", err
	}
	type hit struct {
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		Title    string `json:"title"`
		Subtitle string `json:"subtitle,omitempty"`
		Status   string `json:"status,omitempty"`
	}
	out := []hit{}
	for _, group := range [][]application.SearchHit{results.Clients, results.Projects, results.Tasks, results.Files} {
		for _, h := range group {
			out = append(out, hit{Kind: h.Kind, ID: h.ID.String(), Title: h.Title, Subtitle: h.Subtitle, Status: h.Status})
		}
	}
	return jsonOutput(map[string]any{"results": out})
}

// --- get_client ---

type GetClientTool struct{ clients clientStore }

func NewGetClientTool(clients clientStore) *GetClientTool { return &GetClientTool{clients: clients} }

func (*GetClientTool) Definition() domain.ToolDefinition {
	def, _ := domain.NewToolDefinition("get_client",
		"Lê a ficha de um cliente: dados, situação, contatos, notas e projetos. Só leitura.", domain.RiskLow)
	return def.WithSchema(`{"type":"object","properties":{"clientId":{"type":"string"}},"required":["clientId"],"additionalProperties":false}`,
		"Nenhum: só consulta a ficha do cliente.")
}

func (t *GetClientTool) Execute(ctx context.Context, _ domain.Execution, argsJSON string) (string, error) {
	var args struct {
		ClientID string `json:"clientId"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	id, err := parseClientID(args.ClientID)
	if err != nil {
		return "", err
	}
	client, err := t.clients.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	contacts, err := t.clients.ListContacts(ctx, id)
	if err != nil {
		return "", err
	}
	projects, err := t.clients.ListProjects(ctx, id)
	if err != nil {
		return "", err
	}
	type contact struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	cs := []contact{}
	for _, c := range contacts {
		cs = append(cs, contact{Kind: string(c.Kind), Value: c.Value})
	}
	ps := []string{}
	for _, p := range projects {
		ps = append(ps, p.Name)
	}
	return jsonOutput(map[string]any{
		"id": client.ID.String(), "name": client.Name, "company": client.Company, "status": client.Status,
		"city": client.City, "address": client.Address, "website": client.Website, "notes": client.Notes,
		"optedOut": client.OptedOutAt != nil, "contacts": cs, "projects": ps,
	})
}

// --- add_client_note ---

type AddClientNoteTool struct {
	clients clientStore
	now     func() time.Time
}

func NewAddClientNoteTool(clients clientStore) *AddClientNoteTool {
	return &AddClientNoteTool{clients: clients, now: time.Now}
}

const maxNoteLength = 2000

func (*AddClientNoteTool) Definition() domain.ToolDefinition {
	def, _ := domain.NewToolDefinition("add_client_note",
		"Acrescenta uma nota datada à ficha de um cliente (não apaga as anteriores). Precisa de aprovação do dono.", domain.RiskModerate)
	return def.WithSchema(`{"type":"object","properties":{"clientId":{"type":"string"},"note":{"type":"string"}},"required":["clientId","note"],"additionalProperties":false}`,
		"Altera a ficha do cliente: acrescenta uma nota visível para toda a equipe (as notas anteriores ficam).")
}

func (t *AddClientNoteTool) Execute(ctx context.Context, execution domain.Execution, argsJSON string) (string, error) {
	var args struct {
		ClientID string `json:"clientId"`
		Note     string `json:"note"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	note := strings.TrimSpace(args.Note)
	if note == "" || len(note) > maxNoteLength {
		return "", &domain.ValidationError{Message: "a nota precisa ter de 1 a 2000 caracteres"}
	}
	id, err := parseClientID(args.ClientID)
	if err != nil {
		return "", err
	}
	client, err := t.clients.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	entry := fmt.Sprintf("[%s · agente %s] %s", t.now().Format("02/01/2006 15:04"), execution.AgentID.String(), note)
	notes := entry
	if client.Notes != nil && strings.TrimSpace(*client.Notes) != "" {
		notes = strings.TrimRight(*client.Notes, "\n") + "\n\n" + entry
	}
	if len(notes) > domain.MaxClientNotesLength {
		return "", &domain.ValidationError{Message: "as notas do cliente estão cheias (limite de 10000 caracteres)"}
	}
	client.Notes = &notes
	if _, err := t.clients.Update(ctx, client); err != nil {
		return "", err
	}
	return "nota adicionada ao cliente " + client.Name, nil
}
