package tools

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// Fase T server tools: strictly the servers of the task's own project (a task without a project
// sees the unassigned servers), never the whole fleet.

type serverStore interface {
	FindByID(ctx context.Context, id domain.ServerID) (domain.Server, error)
	ListByProject(ctx context.Context, projectID *uuid.UUID) ([]domain.Server, error)
}

type healthChecker interface {
	Check(ctx context.Context, id domain.ServerID) (application.ServerHealthSnapshot, error)
}

func projectUUID(task domain.Task) *uuid.UUID {
	if task.ProjectID == nil {
		return nil
	}
	id := task.ProjectID.Value
	return &id
}

func sameProject(server domain.Server, task domain.Task) bool {
	if server.ProjectID == nil || task.ProjectID == nil {
		return server.ProjectID == nil && task.ProjectID == nil
	}
	return server.ProjectID.Value == task.ProjectID.Value
}

// --- list_servers ---

type ListServersTool struct {
	tasks   taskReader
	servers serverStore
}

func NewListServersTool(tasks taskReader, servers serverStore) *ListServersTool {
	return &ListServersTool{tasks: tasks, servers: servers}
}

func (*ListServersTool) Definition() domain.ToolDefinition {
	def, _ := domain.NewToolDefinition("list_servers",
		"Lista os servidores do projeto desta task, com o último status conhecido. Só leitura.", domain.RiskLow)
	return def.WithSchema(`{"type":"object","properties":{},"additionalProperties":false}`, "Nenhum: só consulta o cadastro de servidores.")
}

func (t *ListServersTool) Execute(ctx context.Context, execution domain.Execution, _ string) (string, error) {
	task, err := t.tasks.FindByID(ctx, execution.TaskID)
	if err != nil {
		return "", err
	}
	servers, err := t.servers.ListByProject(ctx, projectUUID(task))
	if err != nil {
		return "", err
	}
	type row struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		Host        string  `json:"host"`
		LastStatus  string  `json:"lastStatus"`
		LastChecked *string `json:"lastCheckedAt,omitempty"`
		LastError   *string `json:"lastError,omitempty"`
	}
	out := []row{}
	for _, s := range servers {
		r := row{ID: s.ID.String(), Name: s.Name, Host: s.Host, LastStatus: string(s.LastStatus), LastError: s.LastError}
		if s.LastCheckedAt != nil {
			at := s.LastCheckedAt.UTC().Format(time.RFC3339)
			r.LastChecked = &at
		}
		out = append(out, r)
	}
	return jsonOutput(map[string]any{"servers": out})
}

// --- check_server_health ---

type CheckServerHealthTool struct {
	tasks   taskReader
	servers serverStore
	health  healthChecker
}

func NewCheckServerHealthTool(tasks taskReader, servers serverStore, health healthChecker) *CheckServerHealthTool {
	return &CheckServerHealthTool{tasks: tasks, servers: servers, health: health}
}

func (*CheckServerHealthTool) Definition() domain.ToolDefinition {
	def, _ := domain.NewToolDefinition("check_server_health",
		"Conecta por SSH (credencial já cadastrada, só leitura) e traz uptime, memória, disco e containers de um servidor do projeto.", domain.RiskLow)
	return def.WithSchema(`{"type":"object","properties":{"serverId":{"type":"string"}},"required":["serverId"],"additionalProperties":false}`,
		"Nenhum: só lê o estado do servidor (não reinicia nem altera nada).")
}

func (t *CheckServerHealthTool) Execute(ctx context.Context, execution domain.Execution, argsJSON string) (string, error) {
	var args struct {
		ServerID string `json:"serverId"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	id, err := uuid.Parse(strings.TrimSpace(args.ServerID))
	if err != nil {
		return "", &domain.ValidationError{Message: "serverId deve ser um UUID (use list_servers)"}
	}
	task, err := t.tasks.FindByID(ctx, execution.TaskID)
	if err != nil {
		return "", err
	}
	server, err := t.servers.FindByID(ctx, domain.ServerID{Value: id})
	if err != nil {
		return "", err
	}
	if !sameProject(server, task) {
		// Same answer as "not found": a task never learns about another project's servers.
		return "", application.ErrServerNotFound
	}
	snapshot, err := t.health.Check(ctx, server.ID)
	if err != nil {
		return "", err
	}
	return jsonOutput(map[string]any{
		"server": server.Name, "status": snapshot.Status, "uptime": snapshot.Uptime,
		"memoryUsedMB": snapshot.MemoryUsedMB, "memoryTotalMB": snapshot.MemoryTotalMB,
		"diskUsedPercent": snapshot.DiskUsedPercent, "containers": snapshot.Containers, "error": snapshot.Error,
	})
}
