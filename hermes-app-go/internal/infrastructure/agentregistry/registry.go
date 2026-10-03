package agentregistry

import (
	"sort"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

type Registry struct {
	definitions map[string]domain.AgentDefinition
}

func (r *Registry) Find(agentID domain.AgentID) (domain.AgentDefinition, bool) {
	def, ok := r.definitions[agentID.Value]
	return def, ok
}

func (r *Registry) FindRequired(agentID domain.AgentID) (domain.AgentDefinition, error) {
	def, ok := r.Find(agentID)
	if !ok {
		return domain.AgentDefinition{}, &domain.ErrUnknownAgent{AgentID: agentID.Value}
	}
	return def, nil
}

// List returns the curated catalog sorted by id — what the dashboard offers as a project's
// default agent (Fase PA).
func (r *Registry) List() []domain.AgentDefinition {
	out := make([]domain.AgentDefinition, 0, len(r.definitions))
	for _, def := range r.definitions {
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID.Value < out[j].AgentID.Value })
	return out
}
