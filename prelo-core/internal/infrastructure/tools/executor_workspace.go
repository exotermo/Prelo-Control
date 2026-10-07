package tools

import (
	"context"
	"errors"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// ExecutorWorkspaceTool describes one structured VM operation. Invocation is handled by
// InvokeToolUseCase, which persists the exact request and suspends for Prelo approval. It must
// never execute from prelo-core itself.
type ExecutorWorkspaceTool struct{ def domain.ToolDefinition }

func NewExecutorWorkspaceTools() []application.ToolExecutor {
	items := []struct{ name, description, schema, impact string }{
		{"workspace_start", "Pede um contêiner isolado para esta execução.", `{"type":"object","properties":{},"additionalProperties":false}`, "Cria workspace efêmero isolado da execução."},
		{"workspace_list", "Lista arquivos no workspace isolado.", `{"type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false}`, "Lê nomes e tamanhos de arquivos do workspace."},
		{"workspace_read", "Lê um arquivo pequeno do workspace isolado.", `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`, "Lê até 32 KiB de um arquivo do workspace."},
		{"workspace_mkdir", "Cria uma pasta no workspace isolado.", `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`, "Cria uma pasta sem acesso ao host."},
		{"workspace_create_file", "Cria um arquivo no workspace isolado e publica os mesmos bytes nos Arquivos do projeto.", `{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`, "Cria no máximo 64 KiB e publica no projeto depois de validar a aprovação."},
	}
	out := make([]application.ToolExecutor, 0, len(items))
	for _, item := range items {
		def, _ := domain.NewToolDefinition(item.name, item.description, domain.RiskHigh)
		def = def.WithSchema(item.schema, item.impact)
		out = append(out, &ExecutorWorkspaceTool{def: def})
	}
	return out
}

func (t *ExecutorWorkspaceTool) Definition() domain.ToolDefinition { return t.def }
func (t *ExecutorWorkspaceTool) Execute(context.Context, domain.Execution, string) (string, error) {
	return "", errors.New("workspace operations are executed only by the approved isolated worker")
}
