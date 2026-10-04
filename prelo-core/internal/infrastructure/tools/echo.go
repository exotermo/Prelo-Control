package tools

import (
	"context"

	"github.com/exotermo/prelo-core/internal/domain"
)

// EchoTool exists solely to exercise the approval path end to end (etapa 8): it is
// deliberately harmless (echoes its own input back, still read-only) but classified MODERATE
// so a real REQUIRE_APPROVAL → approve/deny/expire flow has something genuine to run against
// until an actual moderate/high-risk tool exists. Remove once a real one lands.
type EchoTool struct{}

func NewEchoTool() EchoTool { return EchoTool{} }

func (EchoTool) Definition() domain.ToolDefinition {
	def, _ := domain.NewToolDefinition("echo", "Echoes the given args back. Demo tool for the approval workflow, illustrative MODERATE risk only.", domain.RiskModerate)
	return def.WithSchema(`{"type":"object","properties":{"text":{"type":"string"}}}`, "Nenhum: devolve o texto recebido.")
}

func (EchoTool) Execute(_ context.Context, _ domain.Execution, argsJSON string) (string, error) {
	return argsJSON, nil
}
