package toolregistry

import (
	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// Static is a curated, boot-built catalog — mirrors agentregistry's own "never built from
// request input" story.
type Static struct {
	tools map[string]application.ToolExecutor
}

func NewStatic(executors ...application.ToolExecutor) *Static {
	m := make(map[string]application.ToolExecutor, len(executors))
	for _, executor := range executors {
		m[executor.Definition().Name] = executor
	}
	return &Static{tools: m}
}

// Register adds tools whose dependencies only exist later in startup (e.g. the client and
// server repositories); still boot-time only, never from request input.
func (r *Static) Register(executors ...application.ToolExecutor) {
	for _, executor := range executors {
		r.tools[executor.Definition().Name] = executor
	}
}

func (r *Static) Find(name string) (application.ToolExecutor, bool) {
	executor, ok := r.tools[name]
	return executor, ok
}

func (r *Static) List() []domain.ToolDefinition {
	defs := make([]domain.ToolDefinition, 0, len(r.tools))
	for _, executor := range r.tools {
		defs = append(defs, executor.Definition())
	}
	return defs
}
