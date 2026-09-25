package tools

import (
	"context"
	"time"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

// CurrentTimeTool is etapa 7's required read-only demonstration tool: it takes no meaningful
// args and only reads the server clock, so LOW risk is the honest classification — nothing
// here could ever need a human's approval.
type CurrentTimeTool struct{}

func NewCurrentTimeTool() CurrentTimeTool { return CurrentTimeTool{} }

func (CurrentTimeTool) Definition() domain.ToolDefinition {
	def, _ := domain.NewToolDefinition("current_time", "Returns the current server time (UTC, RFC3339). Read-only, no arguments.", domain.RiskLow)
	return def
}

func (CurrentTimeTool) Execute(_ context.Context, _ domain.Execution, _ string) (string, error) {
	return time.Now().UTC().Format(time.RFC3339), nil
}
